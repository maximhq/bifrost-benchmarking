// Package lib provides utility functions and shared types for the Bifrost gateway,
// including account management and debug handlers.
package lib

import (
	"fmt"

	"github.com/maximhq/bifrost/core/schemas"
)

// BaseAccount provides a basic implementation of the schemas.Account interface (partially, tailored for this gateway's needs).
// It stores API keys and configuration details for accessing different AI providers.
// This specific implementation primarily focuses on OpenAI.
type BaseAccount struct {
	apiKey   string // The API key for the primary provider (e.g., OpenAI).
	proxyURL string // URL of an HTTP proxy to be used for outgoing requests, if any.

	concurrency int // Desired concurrency level for requests to the provider.
	bufferSize  int // Buffer size configuration for requests.
}

// NewBaseAccount creates a new instance of BaseAccount.
// Parameters:
//
//	apiKey: The API key for the service provider.
//	proxyURL: The URL string for an HTTP proxy. Can be empty.
//	concurrency: The desired concurrency limit for provider requests.
//	bufferSize: The buffer size to be configured for provider requests.
//
// Returns a pointer to the newly created BaseAccount.
func NewBaseAccount(apiKey string, proxyURL string, concurrency int, bufferSize int) *BaseAccount {
	return &BaseAccount{
		apiKey:      apiKey,
		proxyURL:    proxyURL,
		concurrency: concurrency,
		bufferSize:  bufferSize,
	}
}

// GetKeysForProvider returns the API keys configured for the specified provider.
// For this implementation, it primarily returns the stored apiKey for OpenAI.
// Parameters:
//
//	providerKey: The identifier for the AI provider (e.g., schemas.OpenAI).
//
// Returns a slice of schemas.Key or an error if the provider is unsupported.
func (a *BaseAccount) GetKeysForProvider(providerKey schemas.ModelProvider) ([]schemas.Key, error) {
	if providerKey == schemas.OpenAI {
		return []schemas.Key{
			{
				Value:  a.apiKey,
				Models: []string{"gpt-4o-mini", "gpt-4o", "gpt-4-turbo", "gpt-3.5-turbo"}, // Example models
				Weight: 1.0,
			},
		}, nil
	}

	return nil, fmt.Errorf("unsupported provider in GetKeysForProvider: %s", providerKey)
}

// GetConfiguredProviders returns a list of provider identifiers that this account is configured for.
// Currently, it returns only OpenAI.
func (baseAccount *BaseAccount) GetConfiguredProviders() ([]schemas.ModelProvider, error) {
	return []schemas.ModelProvider{schemas.OpenAI}, nil
}

// GetConfigForProvider returns network and concurrency configurations for the specified provider.
// This includes proxy settings, concurrency limits, and buffer sizes.
// Parameters:
//
//	providerKey: The identifier for the AI provider.
//
// Returns a schemas.ProviderConfig or an error if the provider is unsupported.
func (baseAccount *BaseAccount) GetConfigForProvider(providerKey schemas.ModelProvider) (*schemas.ProviderConfig, error) {
	switch providerKey {
	case schemas.OpenAI:
		config := &schemas.ProviderConfig{
			NetworkConfig: schemas.DefaultNetworkConfig, // Uses default network settings from Bifrost core
			ConcurrencyAndBufferSize: schemas.ConcurrencyAndBufferSize{
				Concurrency: baseAccount.concurrency,
				BufferSize:  baseAccount.bufferSize,
			},
		}

		// Only set proxy configuration if a proxyURL was provided.
		if baseAccount.proxyURL != "" {
			config.ProxyConfig = &schemas.ProxyConfig{
				Type: schemas.HttpProxy,
				URL:  baseAccount.proxyURL,
			}
		}

		return config, nil
	default:
		return nil, fmt.Errorf("unsupported provider in GetConfigForProvider: %s", providerKey)
	}
}
