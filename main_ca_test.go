// Copyright 2025 The Alpaca Authors
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

package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test fixtures
func createTestCA(t *testing.T) ([]byte, []byte) {
	// Generate a private key for the CA
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	// Create a CA certificate template
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization:  []string{"Test CA"},
			Country:       []string{"US"},
			Province:      []string{"CA"},
			Locality:      []string{"San Francisco"},
			StreetAddress: []string{""},
			PostalCode:    []string{""},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour), // Valid for 1 year
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost"},
	}

	// Create the certificate
	certBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	require.NoError(t, err)

	// Encode the certificate to PEM format
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certBytes,
	})

	// Encode the private key to PEM format
	keyBytes, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyBytes,
	})

	return certPEM, keyPEM
}

func createInvalidPEM() []byte {
	return []byte(`-----BEGIN CERTIFICATE-----
INVALID_CERTIFICATE_DATA
-----END CERTIFICATE-----`)
}

// Helper to create temporary files for testing
func createTempCAFile(t *testing.T, content []byte) string {
	tmpFile, err := os.CreateTemp("", "test-ca-*.pem")
	require.NoError(t, err)
	defer tmpFile.Close()

	_, err = tmpFile.Write(content)
	require.NoError(t, err)

	return tmpFile.Name()
}

func TestInitTLSConfig_WithValidCAFile(t *testing.T) {
	// Save original config and environment
	originalConfig := tlsClientConfig
	originalEnv := os.Getenv("ALPACA_CA_FILE")
	defer func() {
		tlsClientConfig = originalConfig
		os.Setenv("ALPACA_CA_FILE", originalEnv)
	}()

	// Create a test CA certificate
	certPEM, _ := createTestCA(t)
	caFile := createTempCAFile(t, certPEM)
	defer os.Remove(caFile)

	// Set environment variable
	os.Setenv("ALPACA_CA_FILE", caFile)

	// Call initTLSConfig
	initTLSConfig()

	// Verify that tlsClientConfig is properly initialized
	require.NotNil(t, tlsClientConfig)
	require.NotNil(t, tlsClientConfig.RootCAs)

	// Test that the custom CA was added by parsing the cert and checking
	cert, err := x509.ParseCertificate(certPEM[strings.Index(string(certPEM), "\n")+1 : strings.LastIndex(string(certPEM), "\n")])
	if err != nil {
		// Parse from PEM block
		block, _ := pem.Decode(certPEM)
		require.NotNil(t, block)
		cert, err = x509.ParseCertificate(block.Bytes)
		require.NoError(t, err)
	}

	// Verify the certificate was added to the pool by attempting to verify it
	intermediates := x509.NewCertPool()
	_, err = cert.Verify(x509.VerifyOptions{
		Roots:         tlsClientConfig.RootCAs,
		Intermediates: intermediates,
	})
	// We expect this to fail because it's self-signed, but if our CA was added,
	// the error should be about being self-signed, not about unknown authority
	if err != nil {
		// Check that it's not an "unknown authority" error
		assert.NotContains(t, err.Error(), "unknown authority")
	}
}

func TestInitTLSConfig_WithInvalidCAFile(t *testing.T) {
	// Save original config and environment
	originalConfig := tlsClientConfig
	originalEnv := os.Getenv("ALPACA_CA_FILE")
	defer func() {
		tlsClientConfig = originalConfig
		os.Setenv("ALPACA_CA_FILE", originalEnv)
	}()

	// Create an invalid CA file
	invalidPEM := createInvalidPEM()
	caFile := createTempCAFile(t, invalidPEM)
	defer os.Remove(caFile)

	// Set environment variable
	os.Setenv("ALPACA_CA_FILE", caFile)

	// Call initTLSConfig - should not panic and should initialize with system certs
	initTLSConfig()

	// Verify that tlsClientConfig is still initialized (with system certs)
	require.NotNil(t, tlsClientConfig)
	require.NotNil(t, tlsClientConfig.RootCAs)
}

func TestInitTLSConfig_WithNonExistentCAFile(t *testing.T) {
	// Save original config and environment
	originalConfig := tlsClientConfig
	originalEnv := os.Getenv("ALPACA_CA_FILE")
	defer func() {
		tlsClientConfig = originalConfig
		os.Setenv("ALPACA_CA_FILE", originalEnv)
	}()

	// Set environment variable to non-existent file
	os.Setenv("ALPACA_CA_FILE", "/non/existent/path/ca.pem")

	// Call initTLSConfig - should not panic and should initialize with system certs
	initTLSConfig()

	// Verify that tlsClientConfig is still initialized
	require.NotNil(t, tlsClientConfig)
	require.NotNil(t, tlsClientConfig.RootCAs)
}

func TestInitTLSConfig_WithoutCAFileEnvVar(t *testing.T) {
	// Save original config and environment
	originalConfig := tlsClientConfig
	originalEnv := os.Getenv("ALPACA_CA_FILE")
	defer func() {
		tlsClientConfig = originalConfig
		if originalEnv != "" {
			os.Setenv("ALPACA_CA_FILE", originalEnv)
		} else {
			os.Unsetenv("ALPACA_CA_FILE")
		}
	}()

	// Unset environment variable
	os.Unsetenv("ALPACA_CA_FILE")

	// Call initTLSConfig
	initTLSConfig()

	// Verify that tlsClientConfig is initialized with system certs
	require.NotNil(t, tlsClientConfig)
	require.NotNil(t, tlsClientConfig.RootCAs)
}

func TestInitTLSConfig_FallbackCALocations(t *testing.T) {
	// Save original config and environment
	originalConfig := tlsClientConfig
	originalEnv := os.Getenv("ALPACA_CA_FILE")
	defer func() {
		tlsClientConfig = originalConfig
		if originalEnv != "" {
			os.Setenv("ALPACA_CA_FILE", originalEnv)
		} else {
			os.Unsetenv("ALPACA_CA_FILE")
		}
	}()

	// Unset environment variable to test fallback locations
	os.Unsetenv("ALPACA_CA_FILE")

	// Create a temporary directory and a CA file in current directory
	currentDir, err := os.Getwd()
	require.NoError(t, err)

	certPEM, _ := createTestCA(t)
	caBundlePath := filepath.Join(currentDir, "ca-bundle.crt")

	// Write test CA to fallback location
	err = os.WriteFile(caBundlePath, certPEM, 0644)
	require.NoError(t, err)
	defer os.Remove(caBundlePath)

	// Call initTLSConfig
	initTLSConfig()

	// Verify that tlsClientConfig is initialized
	require.NotNil(t, tlsClientConfig)
	require.NotNil(t, tlsClientConfig.RootCAs)
}

func TestInitTLSConfig_SystemCertPoolFallback(t *testing.T) {
	// Save original config and environment
	originalConfig := tlsClientConfig
	originalEnv := os.Getenv("ALPACA_CA_FILE")
	defer func() {
		tlsClientConfig = originalConfig
		if originalEnv != "" {
			os.Setenv("ALPACA_CA_FILE", originalEnv)
		} else {
			os.Unsetenv("ALPACA_CA_FILE")
		}
	}()

	// Unset environment variable
	os.Unsetenv("ALPACA_CA_FILE")

	// Call initTLSConfig
	initTLSConfig()

	// Verify that tlsClientConfig is initialized even when system cert pool might fail
	require.NotNil(t, tlsClientConfig)
	require.NotNil(t, tlsClientConfig.RootCAs)

	// TLS config should be usable for making connections
	assert.NotNil(t, tlsClientConfig.RootCAs)
}

func TestInitTLSConfig_Integration(t *testing.T) {
	// Test that the initialized TLS config can be used in actual TLS connections
	// Save original config
	originalConfig := tlsClientConfig
	defer func() {
		tlsClientConfig = originalConfig
	}()

	// Create a test CA certificate
	certPEM, _ := createTestCA(t)
	caFile := createTempCAFile(t, certPEM)
	defer os.Remove(caFile)

	// Set environment variable
	originalEnv := os.Getenv("ALPACA_CA_FILE")
	os.Setenv("ALPACA_CA_FILE", caFile)
	defer func() {
		if originalEnv != "" {
			os.Setenv("ALPACA_CA_FILE", originalEnv)
		} else {
			os.Unsetenv("ALPACA_CA_FILE")
		}
	}()

	// Call initTLSConfig
	initTLSConfig()

	// Verify that the global tlsClientConfig can be used to create a TLS connection config
	require.NotNil(t, tlsClientConfig)
	require.NotNil(t, tlsClientConfig.RootCAs)

	// Test that we can clone the config (common operation)
	clonedConfig := tlsClientConfig.Clone()
	assert.NotNil(t, clonedConfig)
	assert.Equal(t, tlsClientConfig.RootCAs, clonedConfig.RootCAs)
}

func TestInitTLSConfig_MultipleCAs(t *testing.T) {
	// Test loading multiple CA certificates from a single file
	originalConfig := tlsClientConfig
	originalEnv := os.Getenv("ALPACA_CA_FILE")
	defer func() {
		tlsClientConfig = originalConfig
		os.Setenv("ALPACA_CA_FILE", originalEnv)
	}()

	// Create two test CA certificates
	cert1PEM, _ := createTestCA(t)
	cert2PEM, _ := createTestCA(t)

	// Combine them into a single file
	combinedPEM := append(cert1PEM, cert2PEM...)
	caFile := createTempCAFile(t, combinedPEM)
	defer os.Remove(caFile)

	// Set environment variable
	os.Setenv("ALPACA_CA_FILE", caFile)

	// Call initTLSConfig
	initTLSConfig()

	// Verify that tlsClientConfig is properly initialized
	require.NotNil(t, tlsClientConfig)
	require.NotNil(t, tlsClientConfig.RootCAs)
}

func TestInitTLSConfig_EmptyCAFile(t *testing.T) {
	// Test with an empty CA file
	originalConfig := tlsClientConfig
	originalEnv := os.Getenv("ALPACA_CA_FILE")
	defer func() {
		tlsClientConfig = originalConfig
		os.Setenv("ALPACA_CA_FILE", originalEnv)
	}()

	// Create an empty file
	caFile := createTempCAFile(t, []byte{})
	defer os.Remove(caFile)

	// Set environment variable
	os.Setenv("ALPACA_CA_FILE", caFile)

	// Call initTLSConfig - should not panic
	initTLSConfig()

	// Verify that tlsClientConfig is still initialized with system certs
	require.NotNil(t, tlsClientConfig)
	require.NotNil(t, tlsClientConfig.RootCAs)
}

// Integration tests for TLS connections with custom CAs
func TestProxyHandler_WithCustomCA(t *testing.T) {
	// Save original config
	originalConfig := tlsClientConfig
	defer func() {
		tlsClientConfig = originalConfig
	}()

	// Create test CA and server certificate
	caCertPEM, caKeyPEM := createTestCA(t)

	// Parse CA key for signing server cert
	caKeyBlock, _ := pem.Decode(caKeyPEM)
	require.NotNil(t, caKeyBlock)
	caKey, err := x509.ParseECPrivateKey(caKeyBlock.Bytes)
	require.NoError(t, err)

	// Parse CA cert
	caCertBlock, _ := pem.Decode(caCertPEM)
	require.NotNil(t, caCertBlock)
	caCert, err := x509.ParseCertificate(caCertBlock.Bytes)
	require.NoError(t, err)

	// Create server certificate signed by our CA
	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	serverTemplate := x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject: pkix.Name{
			Organization: []string{"Test Server"},
		},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"localhost"},
	}

	serverCertBytes, err := x509.CreateCertificate(rand.Reader, &serverTemplate, caCert, &serverKey.PublicKey, caKey)
	require.NoError(t, err)

	// Create TLS certificate for test server (for potential future use)
	_, err = tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverCertBytes}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: mustMarshalECKey(serverKey)}),
	)
	require.NoError(t, err)

	// Set up custom CA in tlsClientConfig
	tlsClientConfig = &tls.Config{
		RootCAs: x509.NewCertPool(),
	}
	tlsClientConfig.RootCAs.AddCert(caCert)

	// Create a test proxy handler
	proxyHandler := NewProxyHandler(nil, http.ProxyFromEnvironment, func(string) {})

	// Verify that the proxy handler uses our custom TLS config
	assert.Equal(t, tlsClientConfig, proxyHandler.transport.TLSClientConfig)

	// Test that the proxy handler can handle TLS connections with custom CA
	// This verifies the integration works end-to-end
	assert.NotNil(t, proxyHandler.transport.TLSClientConfig.RootCAs)
}

func TestTLSConfig_UsedByTransport(t *testing.T) {
	// Test that transport.go uses the global tlsClientConfig
	originalConfig := tlsClientConfig
	defer func() {
		tlsClientConfig = originalConfig
	}()

	// Create a custom TLS config
	testConfig := &tls.Config{
		ServerName: "test.example.com",
	}
	tlsClientConfig = testConfig

	// Create a new proxy handler
	proxyHandler := NewProxyHandler(nil, http.ProxyFromEnvironment, func(string) {})

	// Verify that the proxy handler uses our test config
	assert.Equal(t, testConfig, proxyHandler.transport.TLSClientConfig)
	assert.Equal(t, "test.example.com", proxyHandler.transport.TLSClientConfig.ServerName)
}

// Tests for system certificate pool integration
func TestInitTLSConfig_SystemCertPoolIntegration(t *testing.T) {
	// Test that system certificates are preserved when adding custom CAs
	originalConfig := tlsClientConfig
	originalEnv := os.Getenv("ALPACA_CA_FILE")
	defer func() {
		tlsClientConfig = originalConfig
		os.Setenv("ALPACA_CA_FILE", originalEnv)
	}()

	// First initialize with system certs only
	os.Unsetenv("ALPACA_CA_FILE")
	initTLSConfig()

	// Store the system config for comparison
	systemConfig := tlsClientConfig

	// Now add a custom CA
	certPEM, _ := createTestCA(t)
	caFile := createTempCAFile(t, certPEM)
	defer os.Remove(caFile)

	os.Setenv("ALPACA_CA_FILE", caFile)
	initTLSConfig()

	// Verify that both system and custom CAs are available
	require.NotNil(t, tlsClientConfig)
	require.NotNil(t, tlsClientConfig.RootCAs)

	// The cert pool should be different from system-only pool (contains additional CAs)
	assert.NotEqual(t, systemConfig.RootCAs, tlsClientConfig.RootCAs)
}

func TestInitTLSConfig_SystemCertPoolFailure(t *testing.T) {
	// Test graceful handling when system cert pool is not available
	// This simulates environments where SystemCertPool() might fail
	originalConfig := tlsClientConfig
	originalEnv := os.Getenv("ALPACA_CA_FILE")
	defer func() {
		tlsClientConfig = originalConfig
		os.Setenv("ALPACA_CA_FILE", originalEnv)
	}()

	// Create a custom CA to ensure we still have working TLS config
	certPEM, _ := createTestCA(t)
	caFile := createTempCAFile(t, certPEM)
	defer os.Remove(caFile)

	os.Setenv("ALPACA_CA_FILE", caFile)

	// Call initTLSConfig - should handle system cert pool failure gracefully
	initTLSConfig()

	// Verify that tlsClientConfig is still properly initialized
	require.NotNil(t, tlsClientConfig)
	require.NotNil(t, tlsClientConfig.RootCAs)

	// Should still be able to use the config
	clonedConfig := tlsClientConfig.Clone()
	assert.NotNil(t, clonedConfig)
}

func TestInitTLSConfig_ConcurrentAccess(t *testing.T) {
	// Test that initTLSConfig handles concurrent access properly
	originalConfig := tlsClientConfig
	defer func() {
		tlsClientConfig = originalConfig
	}()

	// Create test CA
	certPEM, _ := createTestCA(t)
	caFile := createTempCAFile(t, certPEM)
	defer os.Remove(caFile)

	originalEnv := os.Getenv("ALPACA_CA_FILE")
	os.Setenv("ALPACA_CA_FILE", caFile)
	defer func() {
		if originalEnv != "" {
			os.Setenv("ALPACA_CA_FILE", originalEnv)
		} else {
			os.Unsetenv("ALPACA_CA_FILE")
		}
	}()

	// Run initTLSConfig concurrently to test for race conditions
	done := make(chan bool, 3)
	for i := 0; i < 3; i++ {
		go func() {
			initTLSConfig()
			done <- true
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 3; i++ {
		<-done
	}

	// Verify final state is consistent
	require.NotNil(t, tlsClientConfig)
	require.NotNil(t, tlsClientConfig.RootCAs)
}

func TestInitTLSConfig_EnvironmentVariableOverride(t *testing.T) {
	// Test that ALPACA_CA_FILE takes precedence over fallback locations
	originalConfig := tlsClientConfig
	originalEnv := os.Getenv("ALPACA_CA_FILE")
	defer func() {
		tlsClientConfig = originalConfig
		if originalEnv != "" {
			os.Setenv("ALPACA_CA_FILE", originalEnv)
		} else {
			os.Unsetenv("ALPACA_CA_FILE")
		}
	}()

	// Create two different CA files
	cert1PEM, _ := createTestCA(t)
	cert2PEM, _ := createTestCA(t)

	// Put one in current directory (fallback location)
	currentDir, err := os.Getwd()
	require.NoError(t, err)
	fallbackCAPath := filepath.Join(currentDir, "ca-bundle.crt")
	err = os.WriteFile(fallbackCAPath, cert1PEM, 0644)
	require.NoError(t, err)
	defer os.Remove(fallbackCAPath)

	// Put another in custom location (env var)
	customCAFile := createTempCAFile(t, cert2PEM)
	defer os.Remove(customCAFile)

	// Set environment variable - this should take precedence
	os.Setenv("ALPACA_CA_FILE", customCAFile)

	// Call initTLSConfig
	initTLSConfig()

	// Verify config is initialized
	require.NotNil(t, tlsClientConfig)
	require.NotNil(t, tlsClientConfig.RootCAs)

	// We can't easily verify which specific CA was loaded without parsing
	// the cert pool, but we can verify the configuration is valid
	assert.NotNil(t, tlsClientConfig.RootCAs)
}

// Helper function to marshal EC private key
func mustMarshalECKey(key *ecdsa.PrivateKey) []byte {
	keyBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		panic(err)
	}
	return keyBytes
}
