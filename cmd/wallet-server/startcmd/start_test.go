/*
Copyright SecureKey Technologies Inc. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package startcmd // nolint:testpackage // using private types in tests

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io/ioutil"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	mockstorage "github.com/hyperledger/aries-framework-go/pkg/mock/storage"
	ldstore "github.com/hyperledger/aries-framework-go/pkg/store/ld"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/trustbloc/wallet/pkg/restapi/common/store/cookie"
)

type mockServer struct {
	Err error
}

func (s *mockServer) ListenAndServe(host, certFile, keyFile string, handler http.Handler) error {
	return s.Err
}

func TestListenAndServe(t *testing.T) {
	router, err := router(&httpServerParameters{
		oidc:   &oidcParameters{providerURL: mockOIDCProvider(t)},
		tls:    &tlsParameters{},
		cookie: &cookie.Config{},
		keyServer: &keyServerParameters{
			authzKMSURL: "http://localhost",
		},
		agent: &agentParameters{
			dbParam: &dbParam{
				url:    "example.dsn",
				prefix: "sample",
				dbType: "mem",
			},
		},
	})
	require.NoError(t, err)

	h := HTTPServer{}

	err = h.ListenAndServe("localhost:8080", "test.key", "test.cert", router)
	require.Error(t, err)
	require.Contains(t, err.Error(), "open test.key: no such file or directory")
}

func TestSupportedDatabases(t *testing.T) {
	t.Run("aries store", func(t *testing.T) {
		tests := []struct {
			dbURL          string
			dbType         string
			isErr          bool
			expectedErrMsg string
		}{
			{dbURL: "test", dbType: "mem", isErr: false},
			{
				dbURL: "test:test@test/", dbType: "mysql", isErr: true,
				expectedErrMsg: "failed to connect to storage at test:test@test/",
			},
			{
				dbURL: "mongodb://", dbType: "mongodb", isErr: true,
				expectedErrMsg: "failed to connect to storage at mongodb://:",
			},
			{
				dbURL: "random", dbType: "random", isErr: true,
				expectedErrMsg: "key database type not set to a valid type",
			},
		}

		for _, test := range tests {
			_, err := createStoreProviders(&dbParam{
				dbType:  test.dbType,
				prefix:  "hr-store",
				url:     test.dbURL,
				timeout: 1,
			})

			if !test.isErr {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.Contains(t, err.Error(), test.expectedErrMsg)
			}
		}
	})
}

func TestStartCmdContents(t *testing.T) {
	startCmd := GetStartCmd(&mockServer{})

	require.Equal(t, "start", startCmd.Use)
	require.Equal(t, "Start http server", startCmd.Short)
	require.Equal(t, "Start http server", startCmd.Long)

	checkFlagPropertiesCorrect(t, startCmd, hostURLFlagName, hostURLFlagShorthand, hostURLFlagUsage)
}

const invalidArgString = "INVALID"

func validArgs(t *testing.T) map[string]string {
	t.Helper()

	return map[string]string{ // create a fresh map every time, so it can be edited by the test
		hostURLFlagName:                   "localhost:8080",
		tlsCertFileFlagName:               "cert",
		tlsKeyFileFlagName:                "key",
		agentUIURLFlagName:                "ui",
		oidcProviderURLFlagName:           mockOIDCProvider(t),
		oidcClientIDFlagName:              uuid.New().String(),
		oidcClientSecretFlagName:          uuid.New().String(),
		oidcCallbackURLFlagName:           "http://test.com/callback",
		tlsCACertsFlagName:                cert(t),
		sessionCookieAuthKeyFlagName:      key(t),
		sessionCookieEncKeyFlagName:       key(t),
		authzKMSURLFlagName:               "http://localhost",
		opsKMSURLFlagName:                 "http://localhost",
		keyEDVURLFlagName:                 "http://localhost",
		hubAuthURLFlagName:                "http://localhost",
		databaseTypeFlagName:              "mem",
		agentTransportReturnRouteFlagName: "all",
		agentWebSocketReadLimitFlagName:   "65536",
		sessionCookieMaxAgeFlagName:       "100",
	}
}

func argArray(argMap map[string]string) []string {
	args := []string{}

	for k, v := range argMap {
		args = append(args, "--"+k, v)
	}

	return args
}

func TestStartCmdWithBlankArg(t *testing.T) {
	t.Run("test blank host arg", func(t *testing.T) {
		startCmd := GetStartCmd(&mockServer{})

		argMap := validArgs(t)
		argMap[hostURLFlagName] = ""
		args := argArray(argMap)

		startCmd.SetArgs(args)

		err := startCmd.Execute()
		require.Error(t, err)
		require.Equal(t, "host-url value is empty", err.Error())
	})

	t.Run("test blank tls cert arg", func(t *testing.T) {
		startCmd := GetStartCmd(&mockServer{})

		argMap := validArgs(t)
		argMap[tlsCertFileFlagName] = ""
		args := argArray(argMap)

		startCmd.SetArgs(args)

		err := startCmd.Execute()
		require.Error(t, err)
		require.Equal(t, "failed to configure tls cert file: tls-cert-file value is empty", err.Error())
	})

	t.Run("test blank tls key arg", func(t *testing.T) {
		startCmd := GetStartCmd(&mockServer{})

		argMap := validArgs(t)
		argMap[tlsKeyFileFlagName] = ""
		args := argArray(argMap)

		startCmd.SetArgs(args)

		err := startCmd.Execute()
		require.Error(t, err)
		require.Equal(t, "failed to configure tls key file: tls-key-file value is empty", err.Error())
	})
}

func TestStartCmdWithInvalidAgentArgs(t *testing.T) {
	t.Run("test blank dbtype", func(t *testing.T) {
		startCmd := GetStartCmd(&mockServer{})

		argMap := validArgs(t)
		argMap[databaseTypeFlagName] = ""
		args := argArray(argMap)

		startCmd.SetArgs(args)

		err := startCmd.Execute()
		require.Error(t, err)
		require.Equal(t, "database-type value is empty", err.Error())
	})

	t.Run("test invalid db timeout", func(t *testing.T) {
		startCmd := GetStartCmd(&mockServer{})

		argMap := validArgs(t)
		argMap[databaseTimeoutFlagName] = "test123"
		args := argArray(argMap)

		startCmd.SetArgs(args)

		err := startCmd.Execute()
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to parse db timeout")
	})

	t.Run("test invalid http resolver", func(t *testing.T) {
		startCmd := GetStartCmd(&mockServer{})

		argMap := validArgs(t)
		argMap[agentHTTPResolverFlagName] = "-"
		args := argArray(argMap)

		startCmd.SetArgs(args)

		err := startCmd.Execute()
		require.Error(t, err)
		require.Contains(t, err.Error(), "http-resolver-url flag not found")
	})

	t.Run("test invalid inbound transport", func(t *testing.T) {
		startCmd := GetStartCmd(&mockServer{})

		argMap := validArgs(t)
		argMap[agentInboundHostFlagName] = "xys"
		args := argArray(argMap)

		startCmd.SetArgs(args)

		err := startCmd.Execute()
		require.Error(t, err)
		require.Contains(t, err.Error(), "inbound-host flag not found")
	})

	t.Run("test invalid inbound external", func(t *testing.T) {
		startCmd := GetStartCmd(&mockServer{})

		argMap := validArgs(t)
		argMap[agentInboundHostExternalFlagName] = "xys"
		args := argArray(argMap)

		startCmd.SetArgs(args)

		err := startCmd.Execute()
		require.Error(t, err)
		require.Contains(t, err.Error(), "inbound-host-external flag not found")
	})

	t.Run("test invalid transport return", func(t *testing.T) {
		startCmd := GetStartCmd(&mockServer{})

		argMap := validArgs(t)
		argMap[agentTransportReturnRouteFlagName] = "test123"
		args := argArray(argMap)

		startCmd.SetArgs(args)

		err := startCmd.Execute()
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to configure router")
	})

	t.Run("test invalid web socket read limit", func(t *testing.T) {
		startCmd := GetStartCmd(&mockServer{})

		argMap := validArgs(t)
		argMap[agentWebSocketReadLimitFlagName] = "invalid"
		args := argArray(argMap)

		startCmd.SetArgs(args)

		err := startCmd.Execute()
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to parse web socket read limit")
	})

	t.Run("test invalid webhook URL", func(t *testing.T) {
		startCmd := GetStartCmd(&mockServer{})

		argMap := validArgs(t)
		argMap[agentWebhookFlagName] = "testURL"
		args := argArray(argMap)

		startCmd.SetArgs(args)

		err := startCmd.Execute()
		require.Error(t, err)
		require.Contains(t, err.Error(), "webhook-url flag not found")
	})

	t.Run("test invalid outbound transport return", func(t *testing.T) {
		startCmd := GetStartCmd(&mockServer{})

		argMap := validArgs(t)
		argMap[agentOutboundTransportFlagName] = "testReturn"
		args := argArray(argMap)

		startCmd.SetArgs(args)

		err := startCmd.Execute()
		require.Error(t, err)
		require.Contains(t, err.Error(), "outbound-transport flag not found")
	})

	t.Run("invalid session cookie max age value", func(t *testing.T) {
		startCmd := GetStartCmd(&mockServer{})

		argMap := validArgs(t)
		argMap[sessionCookieMaxAgeFlagName] = "INVALID"
		args := argArray(argMap)

		startCmd.SetArgs(args)

		err := startCmd.Execute()
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to parse session cookie max age")
	})

	t.Run("test invalid context provider URL", func(t *testing.T) {
		startCmd := GetStartCmd(&mockServer{})

		argMap := validArgs(t)
		argMap[agentContextProviderFlagName] = "testURL"
		args := argArray(argMap)

		startCmd.SetArgs(args)

		err := startCmd.Execute()
		require.Error(t, err)
		require.Contains(t, err.Error(), "context-provider-url flag not found")
	})
}

func TestCreateAriesAgent(t *testing.T) {
	t.Run("invalid inbound internal host option", func(t *testing.T) {
		_, err := createAriesAgent(&httpServerParameters{agent: &agentParameters{
			dbParam:              &dbParam{dbType: "leveldb"},
			inboundHostInternals: []string{"1@2@3"},
		}, tls: &tlsParameters{}})
		require.Contains(t, err.Error(), "invalid inbound host option")
	})

	t.Run("invalid inbound external host option", func(t *testing.T) {
		_, err := createAriesAgent(&httpServerParameters{agent: &agentParameters{
			dbParam:              &dbParam{dbType: "leveldb"},
			inboundHostExternals: []string{"1@2@3"},
		}, tls: &tlsParameters{}})
		require.Contains(t, err.Error(), "invalid inbound host option")
	})
}

func TestInboundTransportOpts(t *testing.T) {
	t.Run("test agent inbound transport opts", func(t *testing.T) {
		tests := []struct {
			internal []string
			external []string
			error    string
		}{
			{
				internal: []string{"http@localhost", "ws@localhost", "xys@localhost"},
				external: []string{"http@test", "ws@test", "xys@test"},
				error:    "inbound transport [xys] not supported",
			},
			{
				internal: []string{"http@localhost", "ws:localhost"},
				external: []string{"http@test", "ws@test"},
				error:    "inbound internal host : invalid inbound host option: Use scheme@url to pass the option",
			},
			{
				internal: []string{"http@localhost", "ws@localhost"},
				external: []string{"http@test", "ws:test"},
				error:    "inbound external host : invalid inbound host option: Use scheme@url to pass the option",
			},
			{
				internal: []string{"http@localhost", "ws@localhost"},
				external: []string{"http@test", "ws@test"},
			},
		}

		for _, test := range tests {
			_, err := getInboundTransportOpts(test.internal, test.external, "", "", 0)

			if test.error != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), test.error)
			} else {
				require.NoError(t, err)
			}
		}
	})
}

func TestGetOutboundTransportOpts(t *testing.T) {
	_, err := getOutboundTransportOpts([]string{"ws", "http"}, 0)
	require.NoError(t, err)

	_, err = getOutboundTransportOpts([]string{"xyz", "http"}, 0)
	require.Error(t, err)
	require.Equal(t, err.Error(), "outbound transport [xyz] not supported")
}

func TestStartCmdWithMissingArg(t *testing.T) {
	t.Run("test missing host arg", func(t *testing.T) {
		startCmd := GetStartCmd(&mockServer{})

		err := startCmd.Execute()
		require.Error(t, err)
		require.Equal(t,
			"Neither host-url (command line flag) nor HTTP_SERVER_HOST_URL (environment variable) have been set.",
			err.Error())
	})

	t.Run("test invalid auto accept flag", func(t *testing.T) {
		startCmd := GetStartCmd(&mockServer{Err: errors.New("error starting the server")})

		args := argArray(validArgs(t))

		startCmd.SetArgs(args)

		err := startCmd.Execute()
		require.Error(t, err)
		require.Contains(t, err.Error(), "error starting the server")
	})

	t.Run("test invalid tls-cacerts", func(t *testing.T) {
		startCmd := GetStartCmd(&mockServer{})

		argMap := validArgs(t)
		argMap[tlsCACertsFlagName] = invalidArgString
		args := argArray(argMap)

		startCmd.SetArgs(args)

		err := startCmd.Execute()

		require.EqualError(t, err,
			"failed to init tls cert pool: failed to read cert: open INVALID: no such file or directory")
	})

	t.Run("missing oidc provider URL", func(t *testing.T) {
		startCmd := GetStartCmd(&mockServer{})

		argMap := validArgs(t)
		delete(argMap, oidcProviderURLFlagName)
		args := argArray(argMap)

		startCmd.SetArgs(args)

		err := startCmd.Execute()
		require.EqualError(t, err,
			"failed to configure OIDC provider URL: Neither oidc-opurl (command line flag) nor"+
				" HTTP_SERVER_OIDC_OPURL (environment variable) have been set.")
	})

	t.Run("invalid oidc provider URL", func(t *testing.T) {
		startCmd := GetStartCmd(&mockServer{})

		argMap := validArgs(t)
		argMap[oidcProviderURLFlagName] = invalidArgString
		argMap[dependencyMaxRetriesFlagName] = "1"
		args := argArray(argMap)

		startCmd.SetArgs(args)

		err := startCmd.Execute()
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to init OIDC provider")
	})

	t.Run("missing oidc client ID", func(t *testing.T) {
		startCmd := GetStartCmd(&mockServer{})

		argMap := validArgs(t)
		delete(argMap, oidcClientIDFlagName)
		args := argArray(argMap)

		startCmd.SetArgs(args)

		err := startCmd.Execute()
		require.EqualError(t, err,
			"failed to configure OIDC clientID: Neither oidc-clientid (command line flag) nor"+
				" HTTP_SERVER_OIDC_CLIENTID (environment variable) have been set.")
	})

	t.Run("missing oidc client secret", func(t *testing.T) {
		startCmd := GetStartCmd(&mockServer{})

		argMap := validArgs(t)
		delete(argMap, oidcClientSecretFlagName)
		args := argArray(argMap)

		startCmd.SetArgs(args)

		err := startCmd.Execute()
		require.EqualError(t, err,
			"failed to configure OIDC client secret: Neither oidc-clientsecret (command line flag) nor"+
				" HTTP_SERVER_OIDC_CLIENTSECRET (environment variable) have been set.")
	})

	t.Run("missing oidc callback", func(t *testing.T) {
		startCmd := GetStartCmd(&mockServer{})

		argMap := validArgs(t)
		delete(argMap, oidcCallbackURLFlagName)
		args := argArray(argMap)

		startCmd.SetArgs(args)

		err := startCmd.Execute()
		require.EqualError(t, err,
			"failed to configure OIDC callback URL: Neither oidc-callback (command line flag) nor"+
				" HTTP_SERVER_OIDC_CALLBACK (environment variable) have been set.")
	})

	t.Run("missing session cookie auth key", func(t *testing.T) {
		startCmd := GetStartCmd(&mockServer{})

		argMap := validArgs(t)
		delete(argMap, sessionCookieAuthKeyFlagName)
		args := argArray(argMap)

		startCmd.SetArgs(args)

		err := startCmd.Execute()
		require.Error(t, err)
		require.Contains(t, err.Error(),
			"failed to configure session cookie auth key: Neither cookie-auth-key (command line flag) nor"+
				" HTTP_SERVER_COOKIE_AUTH_KEY (environment variable) have been set.")
	})

	t.Run("invalid session cookie auth key path", func(t *testing.T) {
		startCmd := GetStartCmd(&mockServer{})

		argMap := validArgs(t)
		argMap[sessionCookieAuthKeyFlagName] = invalidArgString
		args := argArray(argMap)

		startCmd.SetArgs(args)

		err := startCmd.Execute()
		require.Error(t, err)
		require.Contains(t, err.Error(),
			"failed to configure session cookie auth key: failed to read file INVALID: open INVALID:"+
				" no such file or directory")
	})

	t.Run("invalid session cookie auth key length", func(t *testing.T) {
		startCmd := GetStartCmd(&mockServer{})

		argMap := validArgs(t)
		argMap[sessionCookieAuthKeyFlagName] = invalidKey(t)
		args := argArray(argMap)

		startCmd.SetArgs(args)

		err := startCmd.Execute()
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to configure session cookie auth key")
	})

	t.Run("invalid session cookie enc key length", func(t *testing.T) {
		startCmd := GetStartCmd(&mockServer{})

		argMap := validArgs(t)
		argMap[sessionCookieEncKeyFlagName] = invalidKey(t)
		args := argArray(argMap)

		startCmd.SetArgs(args)

		err := startCmd.Execute()
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to configure session cookie enc key")
	})

	t.Run("missing session cookie enc key", func(t *testing.T) {
		startCmd := GetStartCmd(&mockServer{})

		argMap := validArgs(t)
		delete(argMap, sessionCookieEncKeyFlagName)
		args := argArray(argMap)

		startCmd.SetArgs(args)

		err := startCmd.Execute()
		require.Error(t, err)
		require.Contains(t, err.Error(),
			"failed to configure session cookie enc key: Neither cookie-enc-key (command line flag) nor"+
				" HTTP_SERVER_COOKIE_ENC_KEY (environment variable) have been set.")
	})

	t.Run("invalid log level", func(t *testing.T) {
		startCmd := GetStartCmd(&mockServer{})

		argMap := validArgs(t)
		argMap[agentLogLevelFlagName] = invalidArgString
		args := argArray(argMap)

		startCmd.SetArgs(args)

		err := startCmd.Execute()

		require.Error(t, err)
		require.Contains(t, err.Error(),
			"failed to set log level: failed to parse log level 'INVALID' : logger: invalid log level")
	})

	t.Run("missing authz key server url", func(t *testing.T) {
		startCmd := GetStartCmd(&mockServer{})

		argMap := validArgs(t)
		delete(argMap, authzKMSURLFlagName)
		args := argArray(argMap)

		startCmd.SetArgs(args)

		err := startCmd.Execute()

		require.Error(t, err)
		require.Contains(t, err.Error(),
			"Neither authz-kms-url (command line flag) nor HTTP_SERVER_AUTHZ_KMS_URL (environment variable) have been set.")
	})

	t.Run("missing ops edv server url", func(t *testing.T) {
		startCmd := GetStartCmd(&mockServer{})

		argMap := validArgs(t)
		delete(argMap, keyEDVURLFlagName)
		args := argArray(argMap)

		startCmd.SetArgs(args)

		err := startCmd.Execute()

		require.Error(t, err)
		require.Contains(t, err.Error(),
			"Neither key-edv-url (command line flag) nor HTTP_SERVER_KEY_EDV_URL (environment variable) have been set.")
	})

	t.Run("missing ops key server url", func(t *testing.T) {
		startCmd := GetStartCmd(&mockServer{})

		argMap := validArgs(t)
		delete(argMap, opsKMSURLFlagName)
		args := argArray(argMap)

		startCmd.SetArgs(args)

		err := startCmd.Execute()

		require.Error(t, err)
		require.Contains(t, err.Error(),
			"Neither ops-kms-url (command line flag) nor HTTP_SERVER_OPS_KMS_URL (environment variable) have been set.")
	})

	t.Run("missing hub-auth server url", func(t *testing.T) {
		startCmd := GetStartCmd(&mockServer{})

		argMap := validArgs(t)
		delete(argMap, hubAuthURLFlagName)
		args := argArray(argMap)

		startCmd.SetArgs(args)

		err := startCmd.Execute()

		require.Error(t, err)
		require.Contains(t, err.Error(),
			"Neither hub-auth-url (command line flag) nor HTTP_SERVER_HUB_AUTH_URL (environment variable) have been set.")
	})
}

func TestStartCmdValidArgs(t *testing.T) {
	startCmd := GetStartCmd(&mockServer{})

	args := argArray(validArgs(t))

	startCmd.SetArgs(args)

	err := startCmd.Execute()

	require.NoError(t, err)
}

func TestStartCmdValidArgsEnvVar(t *testing.T) {
	startCmd := GetStartCmd(&mockServer{})

	err := os.Setenv(hostURLEnvKey, "localhost:8080")
	require.NoError(t, err)

	err = os.Setenv(tlsCertFileEnvKey, "cert")
	require.NoError(t, err)

	err = os.Setenv(tlsKeyFileEnvKey, "key")
	require.NoError(t, err)

	err = os.Setenv(agentUIURLEnvKey, "ui")
	require.NoError(t, err)

	err = os.Setenv(oidcProviderURLEnvKey, mockOIDCProvider(t))
	require.NoError(t, err)

	err = os.Setenv(oidcClientIDEnvKey, uuid.New().String())
	require.NoError(t, err)

	err = os.Setenv(oidcClientSecretEnvKey, uuid.New().String())
	require.NoError(t, err)

	err = os.Setenv(oidcCallbackURLEnvKey, "http://test.com/callback")
	require.NoError(t, err)

	err = os.Setenv(sessionCookieEncKeyEnvKey, key(t))
	require.NoError(t, err)

	err = os.Setenv(sessionCookieAuthKeyEnvKey, key(t))
	require.NoError(t, err)

	err = os.Setenv(authzKMSURLEnvKey, "localhost")
	require.NoError(t, err)

	err = os.Setenv(opsKMSURLEnvKey, "localhost")
	require.NoError(t, err)

	err = os.Setenv(keyEDVURLEnvKey, "localhost")
	require.NoError(t, err)

	err = os.Setenv(hubAuthURLEnvKey, "localhost")
	require.NoError(t, err)

	err = os.Setenv(databaseTypeEnvKey, "mem")
	require.NoError(t, err)

	err = startCmd.Execute()

	require.NoError(t, err)
}

func TestStartCmdWithBlankEnvVar(t *testing.T) {
	t.Run("test blank host env var", func(t *testing.T) {
		startCmd := GetStartCmd(&mockServer{})

		err := os.Setenv(hostURLEnvKey, "")
		require.NoError(t, err)

		err = os.Setenv(tlsCertFileEnvKey, "cert")
		require.NoError(t, err)

		err = os.Setenv(tlsKeyFileEnvKey, "key")
		require.NoError(t, err)

		err = startCmd.Execute()
		require.Error(t, err)
		require.Equal(t, "HTTP_SERVER_HOST_URL value is empty", err.Error())
	})
}

func TestHealthCheckHandler(t *testing.T) {
	result := httptest.NewRecorder()
	healthCheckHandler(result, nil)
	require.Equal(t, http.StatusOK, result.Code)
}

func TestCreateVDRs(t *testing.T) {
	tests := []struct {
		name              string
		resolvers         []string
		blocDomain        string
		trustblocResolver string
		expected          int
		accept            map[int][]string
	}{{
		name: "Empty data",
		// expects default trustbloc resolver
		accept:   map[int][]string{0: {"orb"}},
		expected: 1,
	}, {
		name:      "Groups methods by resolver",
		resolvers: []string{"orb@http://resolver.com", "v1@http://resolver.com"},
		accept:    map[int][]string{0: {"orb", "v1"}, 1: {"orb"}},
		// expects resolver.com that supports trustbloc,v1 methods and default trustbloc resolver
		expected: 2,
	}, {
		name:      "Two different resolvers",
		resolvers: []string{"orb@http://resolver1.com", "v1@http://resolver2.com"},
		accept:    map[int][]string{0: {"orb"}, 1: {"v1"}, 2: {"orb"}},
		// expects resolver1.com and resolver2.com that supports trustbloc and v1 methods and default trustbloc resolver
		expected: 3,
	}}

	for _, test := range tests {
		res, err := createVDRs(test.resolvers, test.blocDomain)

		for i, methods := range test.accept {
			for _, method := range methods {
				require.True(t, res[i].Accept(method))
			}
		}

		require.NoError(t, err)
		require.Equal(t, test.expected, len(res))
	}
}

func TestCreateJSONLDDocumentLoader(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		loader, err := createJSONLDDocumentLoader(mockstorage.NewMockStoreProvider())

		require.NotNil(t, loader)
		require.NoError(t, err)
	})

	t.Run("Fail to create JSON-LD context store", func(t *testing.T) {
		storageProvider := mockstorage.NewMockStoreProvider()
		storageProvider.FailNamespace = ldstore.ContextStoreName

		loader, err := createJSONLDDocumentLoader(storageProvider)

		require.Nil(t, loader)
		require.Error(t, err)
		require.Contains(t, err.Error(), "create JSON-LD context store")
	})

	t.Run("Fail to create remote provider store", func(t *testing.T) {
		storageProvider := mockstorage.NewMockStoreProvider()
		storageProvider.FailNamespace = ldstore.RemoteProviderStoreName

		loader, err := createJSONLDDocumentLoader(storageProvider)

		require.Nil(t, loader)
		require.Error(t, err)
		require.Contains(t, err.Error(), "create remote provider store")
	})
}

func checkFlagPropertiesCorrect(t *testing.T, cmd *cobra.Command, flagName, flagShorthand, flagUsage string) {
	t.Helper()

	flag := cmd.Flag(flagName)

	require.NotNil(t, flag)
	require.Equal(t, flagName, flag.Name)
	require.Equal(t, flagShorthand, flag.Shorthand)
	require.Equal(t, flagUsage, flag.Usage)
	require.Equal(t, "", flag.Value.String())

	flagAnnotations := flag.Annotations
	require.Nil(t, flagAnnotations)
}

func mockOIDCProvider(t *testing.T) string {
	t.Helper()

	h := &testOIDCProvider{}
	srv := httptest.NewServer(h)
	h.baseURL = srv.URL

	t.Cleanup(srv.Close)

	return srv.URL
}

type oidcConfigJSON struct {
	Issuer      string   `json:"issuer"`
	AuthURL     string   `json:"authorization_endpoint"`
	TokenURL    string   `json:"token_endpoint"`
	JWKSURL     string   `json:"jwks_uri"`
	UserInfoURL string   `json:"userinfo_endpoint"`
	Algorithms  []string `json:"id_token_signing_alg_values_supported"`
}

type testOIDCProvider struct {
	baseURL string
}

func (t *testOIDCProvider) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	response, err := json.Marshal(&oidcConfigJSON{
		Issuer:      t.baseURL,
		AuthURL:     fmt.Sprintf("%s/oauth2/auth", t.baseURL),
		TokenURL:    fmt.Sprintf("%s/oauth2/token", t.baseURL),
		JWKSURL:     fmt.Sprintf("%s/oauth2/certs", t.baseURL),
		UserInfoURL: fmt.Sprintf("%s/oauth2/userinfo", t.baseURL),
		Algorithms:  []string{"RS256"},
	})
	if err != nil {
		panic(err)
	}

	_, err = w.Write(response)
	if err != nil {
		panic(err)
	}
}

func cert(t *testing.T) string {
	t.Helper()

	file, err := ioutil.TempFile("", "*.pem")
	require.NoError(t, err)

	t.Cleanup(func() {
		fileErr := file.Close()
		require.NoError(t, fileErr)
		fileErr = os.Remove(file.Name())
		require.NoError(t, fileErr)
	})

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	secret, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &secret.PublicKey, secret)
	require.NoError(t, err)

	err = pem.Encode(file, &pem.Block{Type: "CERTIFICATE", Bytes: der})
	require.NoError(t, err)

	return file.Name()
}

func key(t *testing.T) string {
	t.Helper()

	key := make([]byte, 32)

	_, err := rand.Reader.Read(key)
	require.NoError(t, err)

	file, err := ioutil.TempFile("", "test_*.key")
	require.NoError(t, err)

	t.Cleanup(func() {
		delErr := os.Remove(file.Name())
		require.NoError(t, delErr)
	})

	err = ioutil.WriteFile(file.Name(), key, os.ModeAppend)
	require.NoError(t, err)

	return file.Name()
}

func invalidKey(t *testing.T) string {
	t.Helper()

	key := make([]byte, 18)

	n, err := rand.Reader.Read(key)
	require.NoError(t, err)
	require.Equal(t, 18, n)

	file, err := ioutil.TempFile("", "test_*.key")
	require.NoError(t, err)

	t.Cleanup(func() {
		delErr := os.Remove(file.Name())
		require.NoError(t, delErr)
	})

	err = ioutil.WriteFile(file.Name(), key, os.ModeAppend)
	require.NoError(t, err)

	return file.Name()
}
