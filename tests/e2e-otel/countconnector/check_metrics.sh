#!/bin/bash

TOKEN=$(oc create token prometheus-user-workload -n openshift-user-workload-monitoring)
THANOS_QUERIER_HOST=$(oc get route thanos-querier -n openshift-monitoring -o json | jq -r '.spec.host')

# Define the expected values for each metric
metrics=("dev_log_count_total{telemetrygentype=\"logs\"}" \
         "dev_metrics_datapoint_total{telemetrygentype=\"metrics\"}" \
         "dev_span_count_total{telemetrygentype=\"traces\"}" \
         "metric_count_total{telemetrygentype=\"metrics\"}")

values=(1 1 10 1)

# Check metrics for OpenTelemetry collector instance.
for i in "${!metrics[@]}"; do
  metric="${metrics[$i]}"
  expected_value="${values[$i]}"
  actual_value=0

  # Keep fetching and checking the metrics until metrics with the expected value is present.
  while [[ "$actual_value" != "$expected_value" ]]; do
    response=$(curl -k -H "Authorization: Bearer $TOKEN" --data-urlencode "query=${metric}" "https://$THANOS_QUERIER_HOST/api/v1/query")
    #echo "Response for query '${metric}': $response"
    actual_value=$(echo "$response" | jq -r '.data.result[0].value[1]' | tr -d '\n' | tr -d ' ' 2>/dev/null)

    if [[ "$actual_value" != "$expected_value" ]]; then
      echo "Metric '${metric}' does not have the expected value '${expected_value}'. Actual value: '${actual_value:-none}'. Retrying..."
      sleep 5  # Wait for 5 seconds before retrying
    else
      echo "Metric '${metric}' has the expected value '${expected_value}'."
    fi
  done
done
