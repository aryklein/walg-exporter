// Author: Ary Kleinerman

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/joho/godotenv"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type ResponseWALShow struct {
	Status string `json:"status"`
}

type ResponseWalVerify struct {
	Integrity Integrity `json:"integrity"`
}

type Integrity struct {
	Status  string   `json:"status"`
	Details []Detail `json:"details"`
}

type Detail struct {
	TimelineID    int    `json:"timeline_id"`
	StartSegment  string `json:"start_segment"`
	EndSegment    string `json:"end_segment"`
	SegmentsCount int    `json:"segments_count"`
	Status        string `json:"status"`
}

// Define metrics for wal-g wal-verify
var walgVerifyIntegrityStatusMetric = prometheus.NewGaugeVec(prometheus.GaugeOpts{
	Name: "wal_g_verify_integrity_status",
	Help: "wal-g wal-verify integrity status - 0: OK, 1: Error, 2: Unknown",
},
	[]string{"cluster_name"}, // label
)

// Define metrics for wal-g wal-show
var walgShowStatusMetric = prometheus.NewGaugeVec(prometheus.GaugeOpts{
	Name: "wal_g_show_status",
	Help: "wal-g wal-show status - 0: OK, 1: Error, 2: Unknown",
},
	[]string{"cluster_name"}, // label
)

// Define metrics for backup count
var walgBackupCount = prometheus.NewGaugeVec(prometheus.GaugeOpts{
	Name: "wal_g_backup_count",
	Help: "Number of base backups",
},
	[]string{"cluster_name"}, // label
)

// Define metrics for the timestamp of the last uploaded file in the S3 bucket
var lastUploadFileS3TimestampMetric = prometheus.NewGaugeVec(prometheus.GaugeOpts{
	Name: "wal_g_last_upload_file_s3_timestamp",
	Help: "Timestamp of the last wal-g uploaded file in the S3 bucket",
},
	[]string{"cluster_name", "directory"}, // labels
)

// Define global variables
var env string

// Register the metric with Prometheus's default registry
func init() {
	prometheus.MustRegister(walgVerifyIntegrityStatusMetric)
	prometheus.MustRegister(walgShowStatusMetric)
	prometheus.MustRegister(walgBackupCount)
	prometheus.MustRegister(lastUploadFileS3TimestampMetric)

	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}
	env = getEnvOrDefault("ENV", "dev")
}

// getEnvOrDefault returns the value of an environment variable or a default value if not set.
func getEnvOrDefault(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

// Execute the command wal-g backup-list | tail -n +2 | wc -l and return the result
func executeWalgBackupCount(cluster string) float64 {
	envWALE_S3_PREFIX := fmt.Sprintf("WALE_S3_PREFIX=s3://aryklein-pg-backup/postgres/walg/%s-%s/", cluster, env)
	log.Printf("[%s] Executing command: wal-g backup-list | tail -n +2 | wc -l", cluster)
	cmdBackupCount := exec.Command("sh", "-c", "wal-g backup-list | tail -n +2 | wc -l")
	cmdBackupCount.Env = append(os.Environ(), envWALE_S3_PREFIX)
	outputBackupCount, err := cmdBackupCount.Output()
	if err != nil {
		log.Printf("[%s] Error executing command 'wal-g backup-list | tail -n +2 | wc -l': %s", cluster, err)
		return 0
	}

	log.Printf("[%s] wal-g backup count: %s", cluster, string(outputBackupCount))
	backupCount, err := strconv.ParseFloat(strings.TrimSpace(string(outputBackupCount)), 64)
	if err != nil {
		log.Printf("[%s] Error parsing float: %v", cluster, err)
		return 0
	}
	return backupCount
}

// Execute the command wal-g wal-verify integrity and return the result
func executeWalGVerifyInegrity(cluster string) string {
	envPGHOST := fmt.Sprintf("PGHOST=%s.%s.kleinerman.org", cluster, env)
	envWALE_S3_PREFIX := fmt.Sprintf("WALE_S3_PREFIX=s3://aryklein-pg-backup/postgres/walg/%s-%s/", cluster, env)

	log.Printf("[%s] Executing command: wal-g wal-verify integrity --json", cluster)
	cmdWalVerify := exec.Command("wal-g", "wal-verify", "integrity", "--json")
	cmdWalVerify.Env = append(os.Environ(), envPGHOST, envWALE_S3_PREFIX)
	output, err := cmdWalVerify.Output()
	if err != nil {
		log.Printf("[%s] Error executing command 'wal-g wal-verify integrity --json': %s", cluster, err)
		return "Error"
	}

	// Unmarshal the output into the structs
	var responseWalVerify ResponseWalVerify
	if err := json.Unmarshal(output, &responseWalVerify); err != nil {
		log.Printf("[%s] Error unmarshalling JSON: %s", cluster, err)
		return "Error"
	}

	// Extract and print the status
	verifyIntegrityStatusLabel := strings.ToLower(responseWalVerify.Integrity.Status)
	log.Printf("[%s] wal-verify integrity status: %s", cluster, verifyIntegrityStatusLabel)

	return verifyIntegrityStatusLabel
}

func executeWalGShow(cluster string) string {
	envPGHOST := fmt.Sprintf("PGHOST=%s.%s.kleinerman.org", cluster, env)
	envWALE_S3_PREFIX := fmt.Sprintf("WALE_S3_PREFIX=s3://aryklein-pg-backup/postgres/walg/%s-%s/", cluster, env)

	log.Printf("[%s] Executing command: wal-g wal-show --detailed-json", cluster)
	cmdWalShow := exec.Command("wal-g", "wal-show", "--detailed-json")
	cmdWalShow.Env = append(os.Environ(), envPGHOST, envWALE_S3_PREFIX)
	outputWalShow, err := cmdWalShow.Output()
	if err != nil {
		log.Printf("[%s] | Error executing command 'wal-g wal-show --detailed-json': %s", cluster, err)
		return "Error"
	}

	var responseWalShow []ResponseWALShow
	if err := json.Unmarshal([]byte(outputWalShow), &responseWalShow); err != nil {
		log.Printf("[%s] Error unmarshalling JSON: %s", cluster, err)
		return "Error"
	}

	responseWalShowStatusLabel := strings.ToLower(responseWalShow[0].Status)
	log.Printf("[%s] wal-show status: %s", cluster, responseWalShowStatusLabel)

	return responseWalShowStatusLabel
}

// lastUploadFileS3Timestamp returns the timestamp of the last uploaded file in the S3 bucket
// Unix timestamp is used for the last modified time
func lastUploadFileS3Timestamp(cluster string, bucket string, directory string, region string) (int64, error) {
	// Create a new session
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String(region),
	})
	if err != nil {
		return 0, err
	}

	// Create a new S3 client
	svc := s3.New(sess)

	var lastModifiedTimestamp int64

	// Initialize the continuation token
	var continuationToken *string = nil

	// Loop to handle pagination
	for {
		input := &s3.ListObjectsV2Input{
			Bucket:            aws.String(bucket),
			Prefix:            aws.String(directory),
			ContinuationToken: continuationToken,
		}

		// Call ListObjectsV2 directly and handle response and error
		resp, err := svc.ListObjectsV2(input)
		if err != nil {
			return 0, err
		}

		// Loop through the objects and find the last modified timestamp
		for _, item := range resp.Contents {
			itemLastModifiedUnix := item.LastModified.Unix()
			if itemLastModifiedUnix > lastModifiedTimestamp {
				lastModifiedTimestamp = itemLastModifiedUnix
			}
		}

		// Check if the response was truncated and set the continuation token for the next request
		if *resp.IsTruncated {
			continuationToken = resp.NextContinuationToken
		} else {
			break
		}
	}

	log.Printf("[%s] Last modified timestamp: %d", cluster, lastModifiedTimestamp)

	return lastModifiedTimestamp, nil
}

// main function
func main() {
	// Load the .env file for wal-g command
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	// default to 9090 if not set
	port := getEnvOrDefault("WALG_EXPORTER_PORT", "9099")
	// default to 30 if not set
	timer, error := time.ParseDuration(getEnvOrDefault("WALG_EXPORTER_TIMER", "30m"))
	if error != nil {
		log.Printf("Error parsing timer: %s", error)
	}

	// Get the cluster list
	clustersEnv := getEnvOrDefault("PGCLUSTERS", "")
	if clustersEnv == "" {
		log.Fatal("PGCLUSTERS environment variable not set")
	}
	clusters := strings.Split(clustersEnv, ",")

	// Get the S3 bucket region
	s3BucketRegion := getEnvOrDefault("S3_BUCKET_REGION", "us-east-1")

	// Log the timer and port
	log.Printf("Exporter timer set to %s", timer)
	log.Printf("Exporter listens on TCP port %s", port)
	log.Printf("Enabling metrics exposure for the following cluster(s): %s", strings.Join(clusters, ", "))
	log.Printf("S3 bucket region: %s", s3BucketRegion)

	for _, cluster := range clusters {
		// Set up a ticker that fires every 30 minutes
		ticker := time.NewTicker(timer)
		go func(clusterName string) {
			for range ticker.C {
				integrityStatus := executeWalGVerifyInegrity(clusterName)
				switch integrityStatus {
				case "ok":
					walgVerifyIntegrityStatusMetric.With(prometheus.Labels{"cluster_name": clusterName}).Set(0)
				case "error":
					walgVerifyIntegrityStatusMetric.With(prometheus.Labels{"cluster_name": clusterName}).Set(1)
				default:
					walgVerifyIntegrityStatusMetric.With(prometheus.Labels{"cluster_name": clusterName}).Set(2)
				}

				showStatus := executeWalGShow(clusterName)
				switch showStatus {
				case "ok":
					walgShowStatusMetric.With(prometheus.Labels{"cluster_name": clusterName}).Set(0)
				case "error":
					walgShowStatusMetric.With(prometheus.Labels{"cluster_name": clusterName}).Set(1)
				default:
					walgShowStatusMetric.With(prometheus.Labels{"cluster_name": clusterName}).Set(2)
				}

				backupCount := executeWalgBackupCount(clusterName)
				walgBackupCount.With(prometheus.Labels{"cluster_name": clusterName}).Set(backupCount)

				// Get the timestamp of the last uploaded file in the S3 bucket
				lastUploadFileS3Timestamp, err := lastUploadFileS3Timestamp(clusterName, "aryklein-pg-backup", fmt.Sprintf("postgres/walg/%s-%s/", clusterName, env), s3BucketRegion)
				if err != nil {
					log.Printf("[%s] Error getting last upload file S3 timestamp: %s", clusterName, err)
				} else {
					lastUploadFileS3TimestampMetric.With(prometheus.Labels{"cluster_name": clusterName, "directory": fmt.Sprintf("postgres/walg/%s-%s/", clusterName, env)}).Set(float64(lastUploadFileS3Timestamp))
				}
			}
		}(cluster)
	}

	// Start the HTTP server to expose the metrics
	log.Printf("Starting HTTP server on %s...", port)
	// Expose the registered metrics via HTTP.
	http.Handle("/metrics", promhttp.Handler())
	log.Printf("HTTP server started on %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
