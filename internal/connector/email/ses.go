package email

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/aws/aws-sdk-go-v2/service/sesv2/types"
	"github.com/matutetandil/mycel/internal/connector"
)

// SESConnector sends emails via AWS SES
type SESConnector struct {
	name      string
	emailCfg  *Config
	client    *sesv2.Client
}

// NewSESConnector creates a new SES connector
func NewSESConnector(name string, cfg *Config) *SESConnector {
	if cfg.SES == nil {
		cfg.SES = DefaultSESConfig()
	}

	return &SESConnector{
		name:     name,
		emailCfg: cfg,
	}
}

// Name returns the connector name
func (c *SESConnector) Name() string {
	return c.name
}

// Type returns the connector type
func (c *SESConnector) Type() string {
	return "email"
}

// Connect initializes the SES client
func (c *SESConnector) Connect(ctx context.Context) error {
	var opts []func(*config.LoadOptions) error

	// Set region
	if c.emailCfg.SES.Region != "" {
		opts = append(opts, config.WithRegion(c.emailCfg.SES.Region))
	}

	// Set credentials if provided
	if c.emailCfg.SES.AccessKeyID != "" && c.emailCfg.SES.SecretAccessKey != "" {
		creds := credentials.NewStaticCredentialsProvider(
			c.emailCfg.SES.AccessKeyID,
			c.emailCfg.SES.SecretAccessKey,
			"",
		)
		opts = append(opts, config.WithCredentialsProvider(creds))
	}

	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return fmt.Errorf("failed to load AWS config: %w", err)
	}

	c.client = sesv2.NewFromConfig(cfg)
	return nil
}

// Send sends an email via SES
func (c *SESConnector) Send(ctx context.Context, email *Email) (*SendResult, error) {
	// Apply config-level template as default if no per-email template
	if email.Template == "" && c.emailCfg.Template != "" {
		email.Template = c.emailCfg.Template
	}
	// Render HTML template if specified
	if email.Template != "" {
		if err := email.RenderTemplate(nil); err != nil {
			return &SendResult{Success: false, Provider: "ses", Error: err.Error()}, err
		}
	}

	if c.client == nil {
		return &SendResult{
			Success:  false,
			Provider: "ses",
			Error:    "SES client not initialized",
		}, fmt.Errorf("SES client not initialized")
	}

	// Build request
	input, err := c.buildInput(email)
	if err != nil {
		return &SendResult{
			Success:  false,
			Provider: "ses",
			Error:    err.Error(),
		}, err
	}

	// Send
	result, err := c.client.SendEmail(ctx, input)
	if err != nil {
		return &SendResult{
			Success:  false,
			Provider: "ses",
			Error:    err.Error(),
		}, err
	}

	return &SendResult{
		Success:   true,
		Provider:  "ses",
		MessageID: aws.ToString(result.MessageId),
	}, nil
}

func (c *SESConnector) buildInput(email *Email) (*sesv2.SendEmailInput, error) {
	input := &sesv2.SendEmailInput{}

	// From
	from := email.From
	fromName := email.FromName
	if from == "" {
		from = c.emailCfg.From
		fromName = c.emailCfg.FromName
	}
	if from == "" {
		return nil, fmt.Errorf("from address is required")
	}

	if fromName != "" {
		input.FromEmailAddress = aws.String(fmt.Sprintf("%s <%s>", fromName, from))
	} else {
		input.FromEmailAddress = aws.String(from)
	}

	// Destination
	dest := &types.Destination{}

	// To
	for _, r := range email.To {
		if r.Name != "" {
			dest.ToAddresses = append(dest.ToAddresses, fmt.Sprintf("%s <%s>", r.Name, r.Email))
		} else {
			dest.ToAddresses = append(dest.ToAddresses, r.Email)
		}
	}

	// CC
	for _, r := range email.CC {
		if r.Name != "" {
			dest.CcAddresses = append(dest.CcAddresses, fmt.Sprintf("%s <%s>", r.Name, r.Email))
		} else {
			dest.CcAddresses = append(dest.CcAddresses, r.Email)
		}
	}

	// BCC
	for _, r := range email.BCC {
		if r.Name != "" {
			dest.BccAddresses = append(dest.BccAddresses, fmt.Sprintf("%s <%s>", r.Name, r.Email))
		} else {
			dest.BccAddresses = append(dest.BccAddresses, r.Email)
		}
	}

	input.Destination = dest

	// Reply-To
	replyTo := email.ReplyTo
	if replyTo == "" {
		replyTo = c.emailCfg.ReplyTo
	}
	if replyTo != "" {
		input.ReplyToAddresses = []string{replyTo}
	}

	// Content
	if email.TemplateID != "" {
		// Use template
		input.Content = &types.EmailContent{
			Template: &types.Template{
				TemplateName: aws.String(email.TemplateID),
				TemplateData: aws.String(mapToJSON(email.TemplateData)),
			},
		}
	} else {
		// Raw content
		content := &types.EmailContent{
			Simple: &types.Message{
				Subject: &types.Content{
					Data:    aws.String(email.Subject),
					Charset: aws.String("UTF-8"),
				},
				Body: &types.Body{},
			},
		}

		if email.TextBody != "" {
			content.Simple.Body.Text = &types.Content{
				Data:    aws.String(email.TextBody),
				Charset: aws.String("UTF-8"),
			}
		}

		if email.HTMLBody != "" {
			content.Simple.Body.Html = &types.Content{
				Data:    aws.String(email.HTMLBody),
				Charset: aws.String("UTF-8"),
			}
		}

		input.Content = content
	}

	// Configuration set
	if c.emailCfg.SES.ConfigurationSet != "" {
		input.ConfigurationSetName = aws.String(c.emailCfg.SES.ConfigurationSet)
	}

	// Tags
	if len(email.Tags) > 0 {
		for _, tag := range email.Tags {
			input.EmailTags = append(input.EmailTags, types.MessageTag{
				Name:  aws.String("tag"),
				Value: aws.String(tag),
			})
		}
	}

	return input, nil
}

// Write implements connector.Writer interface.
func (c *SESConnector) Write(ctx context.Context, data *connector.Data) (*connector.Result, error) {
	email, err := emailFromData(data.Target, data.Payload)
	if err != nil {
		return nil, err
	}
	result, err := c.Send(ctx, email)
	if err != nil {
		return nil, err
	}
	return &connector.Result{
		Rows:     []map[string]interface{}{{"result": result}},
		Affected: 1,
	}, nil
}

// Health checks SES
func (c *SESConnector) Health(ctx context.Context) error {
	if c.client == nil {
		return fmt.Errorf("SES client not initialized")
	}

	// Try to get account sending quota
	_, err := c.client.GetAccount(ctx, &sesv2.GetAccountInput{})
	return err
}

// Close closes the connector
func (c *SESConnector) Close(ctx context.Context) error {
	return nil
}

func mapToJSON(m map[string]interface{}) string {
	if m == nil {
		return "{}"
	}
	var parts []string
	for k, v := range m {
		switch val := v.(type) {
		case string:
			parts = append(parts, fmt.Sprintf(`"%s":"%s"`, k, val))
		default:
			parts = append(parts, fmt.Sprintf(`"%s":%v`, k, val))
		}
	}
	return "{" + strings.Join(parts, ",") + "}"
}
