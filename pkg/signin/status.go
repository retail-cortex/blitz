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

package signin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	anthropicconfig "github.com/anthropics/anthropic-sdk-go/config"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/retail-cortex/blitz/pkg/config"
)

// googleStatus: Application Default Credentials, the file gcloud's
// sign-in writes (or GOOGLE_APPLICATION_CREDENTIALS names), read here
// without asking Google.
func googleStatus(_ context.Context, _ *config.Config, _ string) (bool, string) {
	path := config.ADCFile()
	if path == "" {
		return false, "no Application Default Credentials"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err.Error()
	}
	var adc struct {
		Type           string `json:"type"`
		QuotaProject   string `json:"quota_project_id"`
		ClientEmail    string `json:"client_email"`
		ServiceAccount string `json:"service_account_impersonation_url"`
	}
	if err := json.Unmarshal(data, &adc); err != nil || adc.Type == "" {
		return false, fmt.Sprintf("%s isn't Application Default Credentials", path)
	}
	detail := adc.Type + ", " + path
	if adc.ClientEmail != "" {
		detail = adc.ClientEmail + " (" + detail + ")"
	}
	if adc.QuotaProject != "" {
		detail += "; quota project " + adc.QuotaProject
	}
	return true, detail
}

// anthropicStatus: the ant profile (profile, else the configured one) has
// credentials.
func anthropicStatus(_ context.Context, cfg *config.Config, profile string) (bool, string) {
	dir, configured := cfg.LLM.Anthropic.OAuthProfile()
	if profile == "" {
		profile = configured
	}
	p, err := anthropicconfig.LoadProfile(dir, profile)
	if err != nil {
		return false, fmt.Sprintf("profile %s: %v", profile, err)
	}
	creds := p.AuthenticationInfo.CredentialsPath
	if info, err := os.Stat(creds); err != nil || info.Size() == 0 {
		return false, fmt.Sprintf("profile %s isn't signed in", profile)
	}
	detail := "profile " + profile
	if u := p.AuthenticationInfo.UserOAuth; u != nil && u.Scope != "" {
		detail += " (" + u.Scope + ")"
	}
	return true, detail
}

// awsStatus: the AWS credential chain Bedrock uses (profile, else the
// configured one) gives credentials now.
func awsStatus(ctx context.Context, cfg *config.Config, profile string) (bool, string) {
	if profile == "" {
		profile = cfg.LLM.Bedrock.Profile
	}
	if profile == "" && !awsSetUp() {
		return false, "no AWS profile or credentials" // without asking the instance metadata service
	}
	var load []func(*awsconfig.LoadOptions) error
	if cfg.LLM.Bedrock.Region != "" {
		load = append(load, awsconfig.WithRegion(cfg.LLM.Bedrock.Region))
	}
	if profile != "" {
		load = append(load, awsconfig.WithSharedConfigProfile(profile))
	}
	aws, err := awsconfig.LoadDefaultConfig(ctx, load...)
	if err != nil {
		return false, err.Error()
	}
	creds, err := aws.Credentials.Retrieve(ctx)
	if err != nil {
		return false, err.Error()
	}
	detail := creds.Source
	if profile != "" {
		detail = "profile " + profile + ", " + detail
	}
	if creds.CanExpire {
		detail += ", until " + creds.Expires.Local().Format("15:04")
	}
	return true, detail
}

// azureScope is the Entra ID scope Azure OpenAI uses.
const azureScope = "https://cognitiveservices.azure.com/.default"

// azureStatus: Entra ID gives a token for Azure OpenAI now, through the
// chain the models use (the Azure CLI's sign-in among them).
func azureStatus(ctx context.Context, _ *config.Config, _ string) (bool, string) {
	if find("az") == "" && find("azd") == "" && !anyEnv("AZURE_CLIENT_ID", "AZURE_FEDERATED_TOKEN_FILE", "IDENTITY_ENDPOINT", "MSI_ENDPOINT") {
		return false, "no Azure CLI sign-in or credentials" // without waiting on managed identity
	}
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return false, err.Error()
	}
	tok, err := cred.GetToken(ctx, policy.TokenRequestOptions{Scopes: []string{azureScope}})
	if err != nil {
		return false, firstLine(err.Error())
	}
	return true, "Entra ID, until " + tok.ExpiresOn.Local().Format("15:04")
}

// awsSetUp reports whether anything for AWS credentials is set up here:
// a profile, keys or a token in the environment, or the shared files.
func awsSetUp() bool {
	if anyEnv("AWS_PROFILE", "AWS_ACCESS_KEY_ID", "AWS_WEB_IDENTITY_TOKEN_FILE", "AWS_CONTAINER_CREDENTIALS_FULL_URI", "AWS_CONTAINER_CREDENTIALS_RELATIVE_URI", "AWS_BEARER_TOKEN_BEDROCK") {
		return true
	}
	home, _ := os.UserHomeDir()
	for _, p := range []string{os.Getenv("AWS_CONFIG_FILE"), os.Getenv("AWS_SHARED_CREDENTIALS_FILE"), filepath.Join(home, ".aws", "config"), filepath.Join(home, ".aws", "credentials")} {
		if p == "" {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

// anyEnv reports whether any of the variables is set.
func anyEnv(keys ...string) bool {
	for _, k := range keys {
		if os.Getenv(k) != "" {
			return true
		}
	}
	return false
}

// firstLine is s up to its first newline (Azure's errors list every
// credential it tried).
func firstLine(s string) string {
	for i, r := range s {
		if r == '\n' {
			return s[:i]
		}
	}
	return s
}
