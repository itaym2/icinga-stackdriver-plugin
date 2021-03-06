package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/golang/protobuf/ptypes/duration"
	"github.com/olorin/nagiosplugin"
	"google.golang.org/api/iterator"

	monitoring "cloud.google.com/go/monitoring/apiv3"
	googlepb "github.com/golang/protobuf/ptypes/timestamp"
	monitoringpb "google.golang.org/genproto/googleapis/monitoring/v3"
)

const (
	checkIntervalInMinutes = 5
)

type options struct {
	filter            string
	project           string
	criticalThreshold int
	warningThreshold  int
}

func main() {
	options := getOptions()

	check := nagiosplugin.NewCheck()
	defer check.Finish()
	check.AddResult(nagiosplugin.OK, "Check succeeded")

	ctx := context.Background()
	client, err := monitoring.NewMetricClient(ctx)

	if err != nil {
		check.AddResult(nagiosplugin.UNKNOWN, "Failed to perform check")
		log.Fatalf("Failed to create client: %v", err)
	}

	intervalStartTime := &googlepb.Timestamp{Seconds: time.Now().Add(-time.Minute * checkIntervalInMinutes).Unix()}
	intervalEndTime := &googlepb.Timestamp{Seconds: time.Now().Unix()}

	request := &monitoringpb.ListTimeSeriesRequest{
		Name:   fmt.Sprintf("projects/%s", options.project),
		Filter: options.filter,
		Interval: &monitoringpb.TimeInterval{
			StartTime: intervalStartTime,
			EndTime:   intervalEndTime,
		},
		Aggregation: &monitoringpb.Aggregation{
			AlignmentPeriod:    &duration.Duration{Seconds: 60 * checkIntervalInMinutes},
			PerSeriesAligner:   monitoringpb.Aggregation_ALIGN_MEAN,
			CrossSeriesReducer: monitoringpb.Aggregation_REDUCE_MEAN,
		},
	}

	it := client.ListTimeSeries(ctx, request)
	handleResult(it, options.criticalThreshold, options.warningThreshold, check)
}

func getOptions() *options {
	filter := flag.String("filter", "", "time series filter")
	project := flag.String("project", "", "name of the google pubsub project containing the monitored resource")
	criticalThreshold := flag.Int("criticalThreshold", -1, "critical alert when result in greater than or equal to this threashold")
	warningThreshold := flag.Int("warningThreshold", -1, "warning alert when result in greater than or equal to this threashold")

	flag.Parse()

	if *filter == "" {
		log.Fatalf("Missing filter param")
	}

	if *project == "" {
		log.Fatalf("Missing project param")
	}

	if *warningThreshold == -1 && *criticalThreshold == -1 {
		log.Fatalf("you must provide either criticalThreshold param or warningThreshold param")
	}

	return &options{
		filter:            *filter,
		project:           *project,
		criticalThreshold: *criticalThreshold,
		warningThreshold:  *warningThreshold,
	}
}

func handleResult(it *monitoring.TimeSeriesIterator, criticalThreshold int, warningThreshold int, check *nagiosplugin.Check) {
	for {
		resp, err := it.Next()
		if err == iterator.Done {
			check.AddResult(nagiosplugin.UNKNOWN, "Failed to perform check, No results returned from stackdriver API")
			break
		}

		if err != nil {
			check.AddResult(nagiosplugin.UNKNOWN, "Failed to perform check")
			log.Fatalf("Failed to fetch time series: %v", err)
		}

		if len(resp.Points) > 1 {
			check.AddResult(nagiosplugin.UNKNOWN, "Failed to perform check, too many points in result")
			log.Fatalf("Response contains more than 1 point, please refine filter and aggregation params so that only 1 point will return")
		}

		value := resp.Points[0].GetValue().GetInt64Value()

		if value > int64(warningThreshold) {
			check.AddResult(nagiosplugin.WARNING, "Result is greater than or equal to warning threshold")
		}

		if value > int64(criticalThreshold) {
			check.AddResult(nagiosplugin.CRITICAL, "Result is greater than or equal to critical threshold")
		}

		break
	}
}
