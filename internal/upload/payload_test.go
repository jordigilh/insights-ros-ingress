package upload

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
)

// TestPayloadFactory provides configurable test data generation
type TestPayloadFactory struct {
	UUID                      string
	ClusterID                 string
	ClusterAlias              string
	Date                      time.Time
	Files                     []string
	ResourceOptimizationFiles []string
	Certified                 bool
	OperatorVersion           string
	IncludeManifest           bool
	IncludeROSFiles           bool
}

// DefaultTestPayloadFactory returns a factory with sensible defaults
func DefaultTestPayloadFactory() *TestPayloadFactory {
	return &TestPayloadFactory{
		UUID:                      "test-uuid-123",
		ClusterID:                 "test-cluster-456",
		ClusterAlias:              "test-cluster",
		Date:                      time.Now(),
		Files:                     []string{"usage.csv"},
		ResourceOptimizationFiles: []string{"ros-data.csv"},
		Certified:                 true,
		OperatorVersion:           "1.0.0",
		IncludeManifest:           true,
		IncludeROSFiles:           true,
	}
}

// WithUUID sets the UUID for the test payload
func (f *TestPayloadFactory) WithUUID(uuid string) *TestPayloadFactory {
	f.UUID = uuid
	return f
}

// WithClusterID sets the cluster ID for the test payload
func (f *TestPayloadFactory) WithClusterID(clusterID string) *TestPayloadFactory {
	f.ClusterID = clusterID
	return f
}

// WithoutManifest excludes the manifest from the payload
func (f *TestPayloadFactory) WithoutManifest() *TestPayloadFactory {
	f.IncludeManifest = false
	return f
}

// WithoutROSFiles excludes ROS files from the payload
func (f *TestPayloadFactory) WithoutROSFiles() *TestPayloadFactory {
	f.ResourceOptimizationFiles = []string{}
	f.IncludeROSFiles = false
	return f
}

// Build creates the test payload bytes
func (f *TestPayloadFactory) Build() ([]byte, error) {
	var buf bytes.Buffer

	// Create gzip writer
	gzipWriter := gzip.NewWriter(&buf)
	defer gzipWriter.Close()

	// Create tar writer
	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	// Add manifest.json if requested
	if f.IncludeManifest {
		manifest := &Manifest{
			UUID:                      f.UUID,
			ClusterID:                 f.ClusterID,
			ClusterAlias:              f.ClusterAlias,
			Date:                      f.Date,
			Files:                     f.Files,
			ResourceOptimizationFiles: f.ResourceOptimizationFiles,
			Certified:                 f.Certified,
			OperatorVersion:           f.OperatorVersion,
		}

		manifestJSON, err := json.Marshal(manifest)
		if err != nil {
			return nil, err
		}

		manifestHeader := &tar.Header{
			Name:     "manifest.json",
			Mode:     0644,
			Size:     int64(len(manifestJSON)),
			Typeflag: tar.TypeReg,
		}

		if err := tarWriter.WriteHeader(manifestHeader); err != nil {
			return nil, err
		}

		if _, err := tarWriter.Write(manifestJSON); err != nil {
			return nil, err
		}
	}

	// Add regular files
	for _, fileName := range f.Files {
		var fileData []byte
		switch fileName {
		case "usage.csv":
			fileData = []byte("timestamp,cpu,memory\n2023-01-01T00:00:00Z,0.5,1024\n")
		default:
			fileData = []byte("test data for " + fileName)
		}

		fileHeader := &tar.Header{
			Name:     fileName,
			Mode:     0644,
			Size:     int64(len(fileData)),
			Typeflag: tar.TypeReg,
		}

		if err := tarWriter.WriteHeader(fileHeader); err != nil {
			return nil, err
		}

		if _, err := tarWriter.Write(fileData); err != nil {
			return nil, err
		}
	}

	// Add ROS files if requested and specified
	if f.IncludeROSFiles {
		for _, rosFileName := range f.ResourceOptimizationFiles {
			var rosData []byte
			switch rosFileName {
			case "ros-data.csv":
				rosData = []byte("node,cpu_request,memory_request\nnode1,100m,256Mi\n")
			default:
				rosData = []byte("ros data for " + rosFileName)
			}

			rosHeader := &tar.Header{
				Name:     rosFileName,
				Mode:     0644,
				Size:     int64(len(rosData)),
				Typeflag: tar.TypeReg,
			}

			if err := tarWriter.WriteHeader(rosHeader); err != nil {
				return nil, err
			}

			if _, err := tarWriter.Write(rosData); err != nil {
				return nil, err
			}
		}
	}

	// Add other files for edge cases
	if !f.IncludeManifest {
		// Add a dummy file when no manifest
		data := []byte("test data")
		header := &tar.Header{
			Name:     "test.txt",
			Mode:     0644,
			Size:     int64(len(data)),
			Typeflag: tar.TypeReg,
		}

		if err := tarWriter.WriteHeader(header); err != nil {
			return nil, err
		}

		if _, err := tarWriter.Write(data); err != nil {
			return nil, err
		}
	}

	// Close writers to flush data
	tarWriter.Close()
	gzipWriter.Close()

	return buf.Bytes(), nil
}

var _ = Describe("PayloadExtractor", func() {
	var (
		extractor *PayloadExtractor
		logger    *logrus.Logger
		tempDir   string
	)

	BeforeEach(func() {
		logger = logrus.New()
		logger.SetLevel(logrus.ErrorLevel) // Suppress logs during tests
		tempDir = GinkgoT().TempDir()
		extractor = NewPayloadExtractor(tempDir, logger)
	})

	Describe("ExtractPayload", func() {
		Context("with valid payload", func() {
			It("should extract payload successfully", func() {
				// Create test payload using factory
				payload, err := DefaultTestPayloadFactory().Build()
				Expect(err).ToNot(HaveOccurred())

				// Extract payload
				result, err := extractor.ExtractPayload(bytes.NewReader(payload), "test-request-123")
				Expect(err).ToNot(HaveOccurred())
				defer result.Cleanup()

				// Verify results
				expected := DefaultTestPayloadFactory()
				Expect(result.Manifest.UUID).To(Equal(expected.UUID))
				Expect(result.Manifest.ClusterID).To(Equal(expected.ClusterID))
				Expect(result.Manifest.ClusterAlias).To(Equal(expected.ClusterAlias))
				Expect(result.Manifest.Certified).To(Equal(expected.Certified))
				Expect(result.Manifest.OperatorVersion).To(Equal(expected.OperatorVersion))
				Expect(result.ROSFiles).To(HaveLen(1))
				Expect(result.ROSFiles).To(HaveKey("ros-data.csv"))
				Expect(result.RequestID).To(Equal("test-request-123"))
			})
		})

		Context("with missing manifest", func() {
			It("should return error when manifest is not found", func() {
				// Create payload without manifest
				payload, err := DefaultTestPayloadFactory().WithoutManifest().Build()
				Expect(err).ToNot(HaveOccurred())

				// Extract payload should fail
				_, err = extractor.ExtractPayload(bytes.NewReader(payload), "test-request-123")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("manifest.json not found"))
			})
		})

		Context("with no ROS files", func() {
			It("should return error when no ROS files are specified", func() {
				// Create payload without ROS files
				payload, err := DefaultTestPayloadFactory().WithoutROSFiles().Build()
				Expect(err).ToNot(HaveOccurred())

				// Extract payload should fail
				_, err = extractor.ExtractPayload(bytes.NewReader(payload), "test-request-123")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("no ROS files"))
			})
		})
	})
})
