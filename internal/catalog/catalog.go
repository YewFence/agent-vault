package catalog

import "github.com/Infisical/agent-vault/internal/broker"

// Category groups templates for browsing in the catalog UI. The value is
// display-ready — the web gallery and CLI print it verbatim.
type Category string

const (
	CategoryAILLM         Category = "AI & LLM"
	CategoryDeveloper     Category = "Developer Tools"
	CategoryCommunication Category = "Communication"
	CategoryCloudInfra    Category = "Cloud & Infra"
	CategoryBusiness      Category = "Business"
)

// Categories is the closed set of valid Category values, in display order.
var Categories = []Category{
	CategoryAILLM,
	CategoryDeveloper,
	CategoryCommunication,
	CategoryCloudInfra,
	CategoryBusiness,
}

// Template represents a preconfigured service template in the catalog.
// Header and Prefix seed api-key auth, Headers seeds custom auth, and
// Substitutions seed the substitution editor independent of AuthType.
// PlaceholderPrefix + PlaceholderLength seed credential-shaped
// placeholder suggestions (see broker.GeneratePlaceholder): prefix is
// the vendor's credential prefix, length the real credential's total
// length (0 = no padding suggestion). Hex-only credential formats are
// left empty — the readable marker would break their charset.
// Aliases are extra search terms for the catalog UI (abbreviations,
// product nicknames) that don't appear in Name or Description.
type Template struct {
	ID                     string                `json:"id"`
	Name                   string                `json:"name"`
	Host                   string                `json:"host"`
	Description            string                `json:"description"`
	Category               Category              `json:"category"`
	Aliases                []string              `json:"aliases,omitempty"`
	AuthType               string                `json:"auth_type"`
	SuggestedCredentialKey string                `json:"suggested_credential_key"`
	Header                 string                `json:"header,omitempty"`
	Prefix                 string                `json:"prefix,omitempty"`
	Headers                map[string]string     `json:"headers,omitempty"`
	Substitutions          []broker.Substitution `json:"substitutions,omitempty"`
	PlaceholderPrefix      string                `json:"placeholder_prefix,omitempty"`
	PlaceholderLength      int                   `json:"placeholder_length,omitempty"`
}

// catalog is the built-in list of common service templates.
var catalog = []Template{
	{ID: "anthropic", Name: "Anthropic", Host: "api.anthropic.com", Description: "Claude API", Category: CategoryAILLM, Aliases: []string{"claude"}, AuthType: "api-key", SuggestedCredentialKey: "ANTHROPIC_API_KEY", Header: "x-api-key", PlaceholderPrefix: "sk-ant-"},
	{ID: "aws-s3", Name: "AWS S3", Host: "s3.amazonaws.com", Description: "Amazon S3 object storage", Category: CategoryCloudInfra, Aliases: []string{"amazon", "s3", "bucket", "storage"}, AuthType: "custom", SuggestedCredentialKey: "AWS_SECRET_ACCESS_KEY"},
	{ID: "cloudflare", Name: "Cloudflare", Host: "api.cloudflare.com", Description: "Cloudflare API", Category: CategoryCloudInfra, Aliases: []string{"cdn", "dns"}, AuthType: "bearer", SuggestedCredentialKey: "CLOUDFLARE_API_TOKEN"},
	{ID: "cohere", Name: "Cohere", Host: "api.cohere.com", Description: "Cohere language models", Category: CategoryAILLM, AuthType: "bearer", SuggestedCredentialKey: "CO_API_KEY"},
	{ID: "datadog", Name: "Datadog", Host: "api.datadoghq.com", Description: "Monitoring and analytics", Category: CategoryCloudInfra, Aliases: []string{"metrics", "logs", "apm"}, AuthType: "api-key", SuggestedCredentialKey: "DATADOG_API_KEY", Header: "DD-API-KEY"},
	{ID: "deepseek", Name: "DeepSeek", Host: "api.deepseek.com", Description: "DeepSeek chat and reasoning models", Category: CategoryAILLM, Aliases: []string{"r1"}, AuthType: "bearer", SuggestedCredentialKey: "DEEPSEEK_API_KEY", PlaceholderPrefix: "sk-"},
	{ID: "discord", Name: "Discord", Host: "discord.com/api/*", Description: "Discord bot and REST API", Category: CategoryCommunication, Aliases: []string{"bot"}, AuthType: "api-key", SuggestedCredentialKey: "DISCORD_BOT_TOKEN", Header: "Authorization", Prefix: "Bot "},
	{ID: "fireworks", Name: "Fireworks AI", Host: "api.fireworks.ai", Description: "Fast open-model inference", Category: CategoryAILLM, AuthType: "bearer", SuggestedCredentialKey: "FIREWORKS_API_KEY"},
	{ID: "gemini", Name: "Google Gemini", Host: "generativelanguage.googleapis.com", Description: "Google Gemini models", Category: CategoryAILLM, Aliases: []string{"google", "bard"}, AuthType: "api-key", SuggestedCredentialKey: "GEMINI_API_KEY", Header: "x-goog-api-key"},
	{ID: "github", Name: "GitHub", Host: "api.github.com", Description: "GitHub REST API", Category: CategoryDeveloper, Aliases: []string{"gh"}, AuthType: "bearer", SuggestedCredentialKey: "GITHUB_TOKEN", PlaceholderPrefix: "github_pat_", PlaceholderLength: 93},
	{ID: "gitlab", Name: "GitLab", Host: "gitlab.com/api/*", Description: "GitLab repos and pipelines", Category: CategoryDeveloper, Aliases: []string{"gl", "ci"}, AuthType: "api-key", SuggestedCredentialKey: "GITLAB_TOKEN", Header: "PRIVATE-TOKEN", PlaceholderPrefix: "glpat-", PlaceholderLength: 26},
	{ID: "groq", Name: "Groq", Host: "api.groq.com", Description: "Fast model inference from Groq", Category: CategoryAILLM, AuthType: "bearer", SuggestedCredentialKey: "GROQ_API_KEY", PlaceholderPrefix: "gsk_"},
	{ID: "jira", Name: "Jira", Host: "*.atlassian.net", Description: "Atlassian Jira project tracking", Category: CategoryDeveloper, Aliases: []string{"atlassian"}, AuthType: "basic", SuggestedCredentialKey: "JIRA_API_TOKEN"},
	{ID: "linear", Name: "Linear", Host: "api.linear.app", Description: "Project management and issue tracking", Category: CategoryDeveloper, Aliases: []string{"issues"}, AuthType: "api-key", SuggestedCredentialKey: "LINEAR_API_KEY", Header: "Authorization"},
	{ID: "mistral", Name: "Mistral AI", Host: "api.mistral.ai", Description: "Mistral chat and embedding models", Category: CategoryAILLM, AuthType: "bearer", SuggestedCredentialKey: "MISTRAL_API_KEY"},
	{ID: "notion", Name: "Notion", Host: "api.notion.com", Description: "Notion workspace API", Category: CategoryBusiness, Aliases: []string{"wiki", "docs", "notes"}, AuthType: "bearer", SuggestedCredentialKey: "NOTION_TOKEN", PlaceholderPrefix: "ntn_"},
	{ID: "npm", Name: "NPM", Host: "registry.npmjs.org", Description: "NPM Default registry", Category: CategoryDeveloper, Aliases: []string{"node", "registry"}, AuthType: "bearer", SuggestedCredentialKey: "NPM_TOKEN", PlaceholderPrefix: "npm_", PlaceholderLength: 40},
	{ID: "npmgh", Name: "Github NPM registry", Host: "npm.pkg.github.com", Description: "Github's NPM registry", Category: CategoryDeveloper, Aliases: []string{"github packages"}, AuthType: "bearer", SuggestedCredentialKey: "NPM_GH_TOKEN", PlaceholderPrefix: "npm_", PlaceholderLength: 40},
	{ID: "openai", Name: "OpenAI", Host: "api.openai.com", Description: "OpenAI / ChatGPT API", Category: CategoryAILLM, Aliases: []string{"chatgpt", "gpt"}, AuthType: "bearer", SuggestedCredentialKey: "OPENAI_API_KEY", PlaceholderPrefix: "sk-"},
	{ID: "openrouter", Name: "OpenRouter", Host: "openrouter.ai", Description: "One key for many AI models", Category: CategoryAILLM, AuthType: "bearer", SuggestedCredentialKey: "OPENROUTER_API_KEY", PlaceholderPrefix: "sk-or-"},
	{ID: "pagerduty", Name: "PagerDuty", Host: "api.pagerduty.com", Description: "Incident management", Category: CategoryCloudInfra, Aliases: []string{"oncall", "on-call"}, AuthType: "custom", SuggestedCredentialKey: "PAGERDUTY_TOKEN", Headers: map[string]string{
		"Authorization": "Token token={{ PAGERDUTY_TOKEN }}",
	}},
	{ID: "perplexity", Name: "Perplexity", Host: "api.perplexity.ai", Description: "Perplexity answer engine", Category: CategoryAILLM, Aliases: []string{"search"}, AuthType: "bearer", SuggestedCredentialKey: "PERPLEXITY_API_KEY"},
	{ID: "postmark", Name: "Postmark", Host: "api.postmarkapp.com", Description: "Transactional email service", Category: CategoryCommunication, Aliases: []string{"email"}, AuthType: "api-key", SuggestedCredentialKey: "POSTMARK_SERVER_TOKEN", Header: "X-Postmark-Server-Token"},
	{ID: "resend", Name: "Resend", Host: "api.resend.com", Description: "Email API for developers", Category: CategoryCommunication, Aliases: []string{"email"}, AuthType: "bearer", SuggestedCredentialKey: "RESEND_API_KEY", PlaceholderPrefix: "re_"},
	{ID: "sendgrid", Name: "SendGrid", Host: "api.sendgrid.com", Description: "Email delivery API", Category: CategoryCommunication, Aliases: []string{"email"}, AuthType: "bearer", SuggestedCredentialKey: "SENDGRID_API_KEY"},
	{ID: "sentry", Name: "Sentry", Host: "sentry.io", Description: "Error tracking and performance monitoring", Category: CategoryDeveloper, Aliases: []string{"errors"}, AuthType: "bearer", SuggestedCredentialKey: "SENTRY_AUTH_TOKEN"},
	{ID: "shopify", Name: "Shopify", Host: "*.myshopify.com", Description: "Shopify e-commerce API", Category: CategoryBusiness, Aliases: []string{"ecommerce", "store"}, AuthType: "api-key", SuggestedCredentialKey: "SHOPIFY_ACCESS_TOKEN", Header: "X-Shopify-Access-Token", PlaceholderPrefix: "shpat_"},
	{ID: "slack", Name: "Slack", Host: "slack.com", Description: "Slack Web API", Category: CategoryCommunication, Aliases: []string{"chat"}, AuthType: "bearer", SuggestedCredentialKey: "SLACK_TOKEN", PlaceholderPrefix: "xoxb-"},
	{ID: "stripe", Name: "Stripe", Host: "api.stripe.com", Description: "Payment processing API", Category: CategoryBusiness, Aliases: []string{"payment", "billing"}, AuthType: "bearer", SuggestedCredentialKey: "STRIPE_SECRET_KEY"},
	{ID: "supabase", Name: "Supabase", Host: "*.supabase.co", Description: "Supabase backend-as-a-service", Category: CategoryDeveloper, Aliases: []string{"postgres", "database"}, AuthType: "api-key", SuggestedCredentialKey: "SUPABASE_KEY", Header: "apikey"},
	{ID: "telegram", Name: "Telegram", Host: "api.telegram.org", Description: "Telegram bot API", Category: CategoryCommunication, Aliases: []string{"bot", "tg"}, AuthType: "passthrough", SuggestedCredentialKey: "TELEGRAM_BOT_TOKEN", Substitutions: []broker.Substitution{
		{Key: "TELEGRAM_BOT_TOKEN", Placeholder: "__TELEGRAM_BOT_TOKEN__", In: []string{"path"}},
	}},
	{ID: "together", Name: "Together AI", Host: "api.together.ai", Description: "Open models on Together AI", Category: CategoryAILLM, AuthType: "bearer", SuggestedCredentialKey: "TOGETHER_API_KEY"},
	{ID: "twilio", Name: "Twilio", Host: "api.twilio.com", Description: "Communication APIs (SMS, voice, email)", Category: CategoryCommunication, Aliases: []string{"sms"}, AuthType: "basic", SuggestedCredentialKey: "TWILIO_AUTH_TOKEN"},
	{ID: "vercel", Name: "Vercel", Host: "api.vercel.com", Description: "Vercel deployment platform", Category: CategoryDeveloper, Aliases: []string{"deploy", "hosting"}, AuthType: "bearer", SuggestedCredentialKey: "VERCEL_TOKEN"},
	{ID: "xai", Name: "xAI (Grok)", Host: "api.x.ai", Description: "Grok models from xAI", Category: CategoryAILLM, Aliases: []string{"grok"}, AuthType: "bearer", SuggestedCredentialKey: "XAI_API_KEY", PlaceholderPrefix: "xai-"},
}

// GetAll returns all available service templates.
func GetAll() []Template {
	return catalog
}

// GetByID returns a template by its ID, or nil if not found.
func GetByID(id string) *Template {
	for i := range catalog {
		if catalog[i].ID == id {
			return &catalog[i]
		}
	}
	return nil
}
