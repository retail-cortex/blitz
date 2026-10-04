// Copyright 2026 Retail Cortex
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package runtime

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/bedrock"
	"github.com/anthropics/anthropic-sdk-go/foundry"
	anthropicoption "github.com/anthropics/anthropic-sdk-go/option"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	openaioption "github.com/openai/openai-go/v3/option"
	"github.com/retail-cortex/blitz/pkg/config"
	"google.golang.org/adk/v2/model"
)

// Models on the clouds (spec_parity_027 PAR-MOD-02): Claude on Amazon
// Bedrock, and OpenAI models and Claude on Azure, each with its cloud's
// standard credentials. Claude on Vertex AI is the anthropic provider with
// auth = "adc" ("vertex-anthropic" names it).

// credentialTimeout bounds checking cloud credentials when a model is
// built, so missing ones fail there, with the reason.
const credentialTimeout = 30 * time.Second

// newBedrockModel is Claude on Amazon Bedrock, signed with the AWS
// credentials the standard chain finds (or AWS_BEARER_TOKEN_BEDROCK).
func newBedrockModel(ctx context.Context, cfg config.BedrockConfig, name string, opts ...anthropicoption.RequestOption) (*anthropicModel, error) {
	name = cmp.Or(name, cfg.Model)
	if name == "" {
		return nil, errors.New("[llm.bedrock] model: the Bedrock model or inference profile, e.g. us.anthropic.claude-sonnet-4-5-20250929-v1:0")
	}
	var load []func(*awsconfig.LoadOptions) error
	if cfg.Region != "" {
		load = append(load, awsconfig.WithRegion(cfg.Region))
	}
	if cfg.Profile != "" {
		load = append(load, awsconfig.WithSharedConfigProfile(cfg.Profile))
	}
	cctx, cancel := context.WithTimeout(ctx, credentialTimeout)
	defer cancel()
	aws, err := awsconfig.LoadDefaultConfig(cctx, load...)
	if err != nil {
		return nil, fmt.Errorf("AWS configuration: %w", err)
	}
	if aws.Region == "" {
		return nil, errors.New("no AWS region: set [llm.bedrock] region, or AWS_REGION")
	}
	if os.Getenv("AWS_BEARER_TOKEN_BEDROCK") == "" {
		if aws.Credentials == nil {
			return nil, errors.New("no AWS credentials: sign in (aws sso login, aws configure) or set AWS_BEARER_TOKEN_BEDROCK")
		}
		if _, err := aws.Credentials.Retrieve(cctx); err != nil {
			return nil, fmt.Errorf("no AWS credentials: %w", err)
		}
	}
	client := anthropic.NewClient(append(opts, bedrock.WithConfig(aws))...)
	return &anthropicModel{client: client, name: name, fallbacks: "off"}, nil
}

// Entra ID scopes.
const (
	azureOpenAIScope = "https://cognitiveservices.azure.com/.default"
)

// azureCredential finds Entra ID credentials: Azure's default chain (the
// environment, workload identity, managed identity, the Azure CLI and
// Developer CLI logins). A variable for tests.
var azureCredential = func() (azcore.TokenCredential, error) {
	return azidentity.NewDefaultAzureCredential(nil)
}

// azureToken is a function returning a fresh Entra ID token for scope,
// after checking one can be had now.
func azureToken(ctx context.Context, scope string) (func(context.Context) (string, error), error) {
	cred, err := azureCredential()
	if err != nil {
		return nil, fmt.Errorf("finding Entra ID credentials: %w", err)
	}
	get := func(ctx context.Context) (string, error) {
		tok, err := cred.GetToken(ctx, policy.TokenRequestOptions{Scopes: []string{scope}})
		return tok.Token, err
	}
	cctx, cancel := context.WithTimeout(ctx, credentialTimeout)
	defer cancel()
	if _, err := get(cctx); err != nil {
		return nil, fmt.Errorf("no Entra ID token (az login, or a managed identity): %w", err)
	}
	return get, nil
}

// isClaude reports whether an Azure deployment serves Claude (through
// Foundry's Anthropic API) rather than an OpenAI model.
func isClaude(name string) bool { return strings.HasPrefix(strings.ToLower(name), "claude") }

// newAzureModel is a model deployed on Azure: Claude through Microsoft
// Foundry, others through Azure OpenAI's v1 API (which speaks OpenAI's).
func newAzureModel(ctx context.Context, cfg config.AzureConfig, name string, pol retryPolicy) (model.LLM, error) {
	name = cmp.Or(name, cfg.Model)
	if name == "" {
		return nil, errors.New("[llm.azure] model: the deployment to use")
	}
	entra := cfg.Auth == config.AuthEntra || cfg.Auth == config.AuthOAuth
	if cfg.Auth != "" && cfg.Auth != config.AuthAPIKey && !entra {
		return nil, fmt.Errorf("unknown [llm.azure] auth %q (api_key or entra)", cfg.Auth)
	}
	if isClaude(name) {
		fc := foundry.ClientConfig{APIKey: cfg.APIKey, Resource: cfg.Resource, BaseURL: cfg.AnthropicBaseURL}
		if fc.BaseURL != "" {
			fc.Resource = ""
		}
		if entra {
			get, err := azureToken(ctx, foundry.EntraIDScope)
			if err != nil {
				return nil, err
			}
			fc.APIKey, fc.AzureADTokenProvider = "", get
		}
		client, err := foundry.NewClient(fc, pol.anthropicOptions()...)
		if err != nil {
			return nil, fmt.Errorf("[llm.azure]: %w", err)
		}
		return &anthropicModel{client: anthropic.NewClient(client.Options...), name: name, fallbacks: "off"}, nil
	}

	base := cfg.BaseURL
	if base == "" {
		if cfg.Resource == "" {
			return nil, errors.New("[llm.azure] resource (or base_url): the Azure OpenAI resource")
		}
		base = "https://" + cfg.Resource + ".openai.azure.com/openai/v1/"
	}
	opts := pol.openAIOptions()
	var header func(context.Context) (string, string, error)
	if entra {
		get, err := azureToken(ctx, azureOpenAIScope)
		if err != nil {
			return nil, err
		}
		header = func(ctx context.Context) (string, string, error) {
			tok, err := get(ctx)
			return "Authorization", "Bearer " + tok, err
		}
	} else {
		key := cmp.Or(cfg.APIKey, os.Getenv("AZURE_OPENAI_API_KEY"))
		if key == "" {
			return nil, errors.New("no Azure OpenAI key: set [llm.azure] api_key or AZURE_OPENAI_API_KEY, or auth = \"entra\"")
		}
		header = func(context.Context) (string, string, error) { return "api-key", key, nil }
	}
	opts = append(opts, openaioption.WithMiddleware(func(req *http.Request, next openaioption.MiddlewareNext) (*http.Response, error) {
		k, v, err := header(req.Context())
		if err != nil {
			return nil, err
		}
		req.Header.Del("Authorization")
		req.Header.Set(k, v)
		return next(req)
	}))
	return newOpenAIModel(ctx, name, "azure", base, opts...)
}
