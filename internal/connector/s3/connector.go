package s3

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/matutetandil/mycel/internal/connector"
)

// Connector implements the connector.Connector interface for S3.
type Connector struct {
	name   string
	config *Config
	client *s3.Client
}

// New creates a new S3 connector.
func New(name string, cfg *Config) *Connector {
	return &Connector{
		name:   name,
		config: cfg,
	}
}

// Name returns the connector name.
func (c *Connector) Name() string {
	return c.name
}

// Type returns the connector type.
func (c *Connector) Type() string {
	return "s3"
}

// Connect initializes the S3 client.
func (c *Connector) Connect(ctx context.Context) error {
	var opts []func(*config.LoadOptions) error

	// Set region
	if c.config.Region != "" {
		opts = append(opts, config.WithRegion(c.config.Region))
	}

	// Set credentials if provided
	if c.config.AccessKey != "" && c.config.SecretKey != "" {
		creds := credentials.NewStaticCredentialsProvider(
			c.config.AccessKey,
			c.config.SecretKey,
			c.config.SessionToken,
		)
		opts = append(opts, config.WithCredentialsProvider(creds))
	}

	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create S3 client with optional custom endpoint
	s3Opts := []func(*s3.Options){}
	if c.config.Endpoint != "" {
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(c.config.Endpoint)
			o.UsePathStyle = c.config.UsePathStyle
		})
	}

	c.client = s3.NewFromConfig(cfg, s3Opts...)
	return nil
}

// Close closes the S3 connection.
func (c *Connector) Close(ctx context.Context) error {
	// S3 client doesn't require explicit closing
	return nil
}

// Health checks the S3 connection health.
func (c *Connector) Health(ctx context.Context) error {
	if c.client == nil {
		return fmt.Errorf("s3 client not connected")
	}

	// Try to list buckets as a health check
	_, err := c.client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(c.config.Bucket),
	})
	return err
}

// Read reads an object from S3 and returns its contents.
func (c *Connector) Read(ctx context.Context, query *connector.Query) ([]map[string]interface{}, error) {
	if c.client == nil {
		if err := c.Connect(ctx); err != nil {
			return nil, err
		}
	}

	key := c.buildKey(query.Target)
	format := c.getFormat(key, query.Params)

	// Check if this is a list operation
	if query.Target == "" || strings.HasSuffix(query.Target, "/") || query.Target == "." {
		return c.listObjects(ctx, key)
	}

	// Get the object
	result, err := c.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.config.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get object %s: %w", key, err)
	}
	defer result.Body.Close()

	data, err := io.ReadAll(result.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read object body: %w", err)
	}

	return c.parseContent(data, format)
}

// Write writes content to an S3 object.
func (c *Connector) Write(ctx context.Context, data *connector.Data) (map[string]interface{}, error) {
	if c.client == nil {
		if err := c.Connect(ctx); err != nil {
			return nil, err
		}
	}

	key := c.buildKey(data.Target)
	format := c.getFormat(key, data.Params)

	content, ok := data.Params["content"]
	if !ok {
		return nil, fmt.Errorf("content is required for write operation")
	}

	// Serialize content based on format
	var body []byte
	var contentType string
	var err error

	switch format {
	case "json":
		body, err = json.MarshalIndent(content, "", "  ")
		contentType = "application/json"
	case "csv":
		body, err = c.serializeCSV(content)
		contentType = "text/csv"
	case "text", "lines":
		body = []byte(fmt.Sprintf("%v", content))
		contentType = "text/plain"
	case "binary":
		if b, ok := content.([]byte); ok {
			body = b
		} else if s, ok := content.(string); ok {
			body = []byte(s)
		} else {
			body, err = json.Marshal(content)
		}
		contentType = "application/octet-stream"
	default:
		body, err = json.MarshalIndent(content, "", "  ")
		contentType = "application/json"
	}

	if err != nil {
		return nil, fmt.Errorf("failed to serialize content: %w", err)
	}

	// Override content type if specified
	if ct, ok := data.Params["content_type"].(string); ok {
		contentType = ct
	}

	// Build metadata
	metadata := make(map[string]string)
	if m, ok := data.Params["metadata"].(map[string]interface{}); ok {
		for k, v := range m {
			metadata[k] = fmt.Sprintf("%v", v)
		}
	}

	// Put the object
	input := &s3.PutObjectInput{
		Bucket:      aws.String(c.config.Bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(body),
		ContentType: aws.String(contentType),
	}

	if len(metadata) > 0 {
		input.Metadata = metadata
	}

	// Set storage class if specified
	if sc, ok := data.Params["storage_class"].(string); ok {
		input.StorageClass = types.StorageClass(sc)
	}

	// Set ACL if specified
	if acl, ok := data.Params["acl"].(string); ok {
		input.ACL = types.ObjectCannedACL(acl)
	}

	result, err := c.client.PutObject(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to put object %s: %w", key, err)
	}

	return map[string]interface{}{
		"key":      key,
		"bucket":   c.config.Bucket,
		"etag":     aws.ToString(result.ETag),
		"size":     len(body),
		"uploaded": true,
	}, nil
}

// Call executes an S3 operation.
func (c *Connector) Call(ctx context.Context, operation string, input map[string]interface{}) (interface{}, error) {
	if c.client == nil {
		if err := c.Connect(ctx); err != nil {
			return nil, err
		}
	}

	switch operation {
	case "exists":
		return c.exists(ctx, input)
	case "head", "stat":
		return c.head(ctx, input)
	case "copy":
		return c.copyObject(ctx, input)
	case "move":
		return c.moveObject(ctx, input)
	case "delete":
		return c.deleteObject(ctx, input)
	case "list":
		return c.listObjectsCall(ctx, input)
	case "presign_get":
		return c.presignGet(ctx, input)
	case "presign_put":
		return c.presignPut(ctx, input)
	default:
		return nil, fmt.Errorf("unknown operation: %s", operation)
	}
}

// Helper methods

func (c *Connector) buildKey(target string) string {
	if c.config.Prefix != "" {
		return path.Join(c.config.Prefix, target)
	}
	return target
}

func (c *Connector) getFormat(key string, params map[string]interface{}) string {
	if f, ok := params["format"].(string); ok {
		return f
	}
	if c.config.Format != "" {
		return c.config.Format
	}
	// Detect from extension
	ext := strings.ToLower(path.Ext(key))
	switch ext {
	case ".json":
		return "json"
	case ".csv":
		return "csv"
	case ".txt", ".log", ".md":
		return "text"
	default:
		return "binary"
	}
}

func (c *Connector) parseContent(data []byte, format string) ([]map[string]interface{}, error) {
	switch format {
	case "json":
		var result interface{}
		if err := json.Unmarshal(data, &result); err != nil {
			return nil, fmt.Errorf("failed to parse JSON: %w", err)
		}
		// Handle both arrays and objects
		switch v := result.(type) {
		case []interface{}:
			rows := make([]map[string]interface{}, len(v))
			for i, item := range v {
				if m, ok := item.(map[string]interface{}); ok {
					rows[i] = m
				} else {
					rows[i] = map[string]interface{}{"value": item}
				}
			}
			return rows, nil
		case map[string]interface{}:
			return []map[string]interface{}{v}, nil
		default:
			return []map[string]interface{}{{"value": v}}, nil
		}

	case "csv":
		reader := csv.NewReader(bytes.NewReader(data))
		records, err := reader.ReadAll()
		if err != nil {
			return nil, fmt.Errorf("failed to parse CSV: %w", err)
		}
		if len(records) == 0 {
			return []map[string]interface{}{}, nil
		}
		headers := records[0]
		rows := make([]map[string]interface{}, 0, len(records)-1)
		for _, record := range records[1:] {
			row := make(map[string]interface{})
			for j, value := range record {
				if j < len(headers) {
					row[headers[j]] = value
				}
			}
			rows = append(rows, row)
		}
		return rows, nil

	case "lines":
		lines := strings.Split(string(data), "\n")
		rows := make([]map[string]interface{}, 0, len(lines))
		for i, line := range lines {
			if line != "" {
				rows = append(rows, map[string]interface{}{
					"line":    i + 1,
					"content": line,
				})
			}
		}
		return rows, nil

	case "text":
		return []map[string]interface{}{
			{"content": string(data)},
		}, nil

	case "binary":
		return []map[string]interface{}{
			{"content": data, "size": len(data)},
		}, nil

	default:
		return []map[string]interface{}{
			{"content": string(data)},
		}, nil
	}
}

func (c *Connector) serializeCSV(content interface{}) ([]byte, error) {
	rows, ok := content.([]map[string]interface{})
	if !ok {
		if arr, ok := content.([]interface{}); ok {
			rows = make([]map[string]interface{}, len(arr))
			for i, item := range arr {
				if m, ok := item.(map[string]interface{}); ok {
					rows[i] = m
				}
			}
		} else {
			return nil, fmt.Errorf("CSV content must be an array of objects")
		}
	}

	if len(rows) == 0 {
		return []byte{}, nil
	}

	// Get headers from first row
	var headers []string
	for key := range rows[0] {
		headers = append(headers, key)
	}

	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)

	// Write headers
	if err := writer.Write(headers); err != nil {
		return nil, err
	}

	// Write data
	for _, row := range rows {
		record := make([]string, len(headers))
		for i, header := range headers {
			record[i] = fmt.Sprintf("%v", row[header])
		}
		if err := writer.Write(record); err != nil {
			return nil, err
		}
	}

	writer.Flush()
	return buf.Bytes(), writer.Error()
}

func (c *Connector) listObjects(ctx context.Context, prefix string) ([]map[string]interface{}, error) {
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(c.config.Bucket),
	}
	if prefix != "" && prefix != "." {
		input.Prefix = aws.String(prefix)
	}

	result, err := c.client.ListObjectsV2(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to list objects: %w", err)
	}

	objects := make([]map[string]interface{}, 0, len(result.Contents))
	for _, obj := range result.Contents {
		objects = append(objects, map[string]interface{}{
			"key":           aws.ToString(obj.Key),
			"size":          obj.Size,
			"last_modified": obj.LastModified.Format(time.RFC3339),
			"etag":          strings.Trim(aws.ToString(obj.ETag), "\""),
			"storage_class": string(obj.StorageClass),
		})
	}

	return objects, nil
}

// Operation implementations

func (c *Connector) exists(ctx context.Context, input map[string]interface{}) (interface{}, error) {
	key := c.buildKey(fmt.Sprintf("%v", input["key"]))
	_, err := c.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(c.config.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		// Check if it's a not found error
		return map[string]interface{}{"exists": false, "key": key}, nil
	}
	return map[string]interface{}{"exists": true, "key": key}, nil
}

func (c *Connector) head(ctx context.Context, input map[string]interface{}) (interface{}, error) {
	key := c.buildKey(fmt.Sprintf("%v", input["key"]))
	result, err := c.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(c.config.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to head object %s: %w", key, err)
	}

	return map[string]interface{}{
		"key":           key,
		"size":          aws.ToInt64(result.ContentLength),
		"content_type":  aws.ToString(result.ContentType),
		"last_modified": result.LastModified.Format(time.RFC3339),
		"etag":          strings.Trim(aws.ToString(result.ETag), "\""),
		"metadata":      result.Metadata,
	}, nil
}

func (c *Connector) copyObject(ctx context.Context, input map[string]interface{}) (interface{}, error) {
	source := c.buildKey(fmt.Sprintf("%v", input["source"]))
	dest := c.buildKey(fmt.Sprintf("%v", input["destination"]))

	copySource := fmt.Sprintf("%s/%s", c.config.Bucket, source)

	_, err := c.client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:     aws.String(c.config.Bucket),
		CopySource: aws.String(copySource),
		Key:        aws.String(dest),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to copy object: %w", err)
	}

	return map[string]interface{}{
		"copied": true,
		"source": source,
		"dest":   dest,
	}, nil
}

func (c *Connector) moveObject(ctx context.Context, input map[string]interface{}) (interface{}, error) {
	// Copy first
	result, err := c.copyObject(ctx, input)
	if err != nil {
		return nil, err
	}

	// Then delete source
	source := c.buildKey(fmt.Sprintf("%v", input["source"]))
	_, err = c.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(c.config.Bucket),
		Key:    aws.String(source),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to delete source after move: %w", err)
	}

	res := result.(map[string]interface{})
	res["moved"] = true
	delete(res, "copied")
	return res, nil
}

func (c *Connector) deleteObject(ctx context.Context, input map[string]interface{}) (interface{}, error) {
	key := c.buildKey(fmt.Sprintf("%v", input["key"]))
	_, err := c.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(c.config.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to delete object %s: %w", key, err)
	}

	return map[string]interface{}{
		"deleted": true,
		"key":     key,
	}, nil
}

func (c *Connector) listObjectsCall(ctx context.Context, input map[string]interface{}) (interface{}, error) {
	prefix := ""
	if p, ok := input["prefix"].(string); ok {
		prefix = c.buildKey(p)
	}
	return c.listObjects(ctx, prefix)
}

func (c *Connector) presignGet(ctx context.Context, input map[string]interface{}) (interface{}, error) {
	key := c.buildKey(fmt.Sprintf("%v", input["key"]))
	expires := 15 * time.Minute
	if e, ok := input["expires"].(string); ok {
		if d, err := time.ParseDuration(e); err == nil {
			expires = d
		}
	}

	presigner := s3.NewPresignClient(c.client)
	result, err := presigner.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.config.Bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(expires))
	if err != nil {
		return nil, fmt.Errorf("failed to presign get: %w", err)
	}

	return map[string]interface{}{
		"url":     result.URL,
		"method":  result.Method,
		"expires": expires.String(),
	}, nil
}

func (c *Connector) presignPut(ctx context.Context, input map[string]interface{}) (interface{}, error) {
	key := c.buildKey(fmt.Sprintf("%v", input["key"]))
	expires := 15 * time.Minute
	if e, ok := input["expires"].(string); ok {
		if d, err := time.ParseDuration(e); err == nil {
			expires = d
		}
	}

	presigner := s3.NewPresignClient(c.client)
	result, err := presigner.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(c.config.Bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(expires))
	if err != nil {
		return nil, fmt.Errorf("failed to presign put: %w", err)
	}

	return map[string]interface{}{
		"url":     result.URL,
		"method":  result.Method,
		"expires": expires.String(),
	}, nil
}
