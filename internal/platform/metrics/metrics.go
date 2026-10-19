package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Fixed enum values for dispatch_requests_total result label.
const (
	ResultSuccess   = "success"
	ResultNoDriver  = "no_driver"
	ResultError     = "error"
	ResultDuplicate = "duplicate"
)

var (
	// DispatchRequestsTotal tracks ride request outcomes.
	DispatchRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dispatch_requests_total",
			Help: "Total count of dispatch ride requests processed partitioned by outcome result.",
		},
		[]string{"result"},
	)

	// DispatchLatencySeconds tracks end-to-end dispatch duration with exact SLO boundary buckets.
	DispatchLatencySeconds = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "dispatch_latency_seconds",
			Help:    "Latency of dispatch requests in seconds.",
			Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10},
		},
	)

	// DispatchNoDriverTotal counts requests failing due to no available candidate.
	DispatchNoDriverTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "dispatch_no_driver_total",
			Help: "Total count of requests where no driver was found in the search radius.",
		},
	)

	// DispatchDoubleBookingTotal must stay strictly 0 in production.
	DispatchDoubleBookingTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "dispatch_double_booking_total",
			Help: "Count of double bookings detected. Must strictly stay 0.",
		},
	)

	// DispatchReassignmentsTotal tracks reassignments triggered by timeout or driver rejection.
	DispatchReassignmentsTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "dispatch_reassignments_total",
			Help: "Total number of driver reassignments executed.",
		},
	)

	// DriverLocationUpdatesTotal tracks location ingestion throughput.
	DriverLocationUpdatesTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "driver_location_updates_total",
			Help: "Total number of driver location updates successfully ingested.",
		},
	)

	// ActiveDrivers tracks the gauge of currently active/available drivers.
	ActiveDrivers = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "active_drivers",
			Help: "Current count of active drivers sending heartbeat location updates.",
		},
	)

	// KafkaConsumerLag tracks consumer group offset lag per topic.
	KafkaConsumerLag = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_consumer_lag",
			Help: "Kafka consumer lag by group and topic.",
		},
		[]string{"group", "topic"},
	)

	// OutboxPending tracks uncommitted/un-streamed outbox events.
	OutboxPending = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "outbox_pending",
			Help: "Count of pending outbox events awaiting transmission to Kafka.",
		},
	)

	// PushFailuresTotal tracks mobile push delivery failures.
	PushFailuresTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "push_failures_total",
			Help: "Total count of push notification delivery errors.",
		},
	)
)
