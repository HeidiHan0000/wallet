/*
Copyright SecureKey Technologies Inc. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package startcmd

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"github.com/cenkalti/backoff/v4"
	oidcp "github.com/coreos/go-oidc"
	"github.com/gorilla/mux"
	ariescouchdb "github.com/hyperledger/aries-framework-go-ext/component/storage/couchdb"
	ariesmongodb "github.com/hyperledger/aries-framework-go-ext/component/storage/mongodb"
	ariesmysql "github.com/hyperledger/aries-framework-go-ext/component/storage/mysql"
	ariesleveldb "github.com/hyperledger/aries-framework-go/component/storage/leveldb"
	ariesmem "github.com/hyperledger/aries-framework-go/component/storageutil/mem"
	ldrest "github.com/hyperledger/aries-framework-go/pkg/controller/rest/ld"
	"github.com/hyperledger/aries-framework-go/pkg/didcomm/messaging/msghandler"
	"github.com/hyperledger/aries-framework-go/pkg/doc/ld"
	ldsvc "github.com/hyperledger/aries-framework-go/pkg/ld"
	ldstore "github.com/hyperledger/aries-framework-go/pkg/store/ld"
	ariesstorage "github.com/hyperledger/aries-framework-go/spi/storage"
	jsonld "github.com/piprate/json-gold/ld"
	"github.com/rs/cors"
	"github.com/spf13/cobra"
	"github.com/trustbloc/edge-core/pkg/log"
	cmdutils "github.com/trustbloc/edge-core/pkg/utils/cmd"
	tlsutils "github.com/trustbloc/edge-core/pkg/utils/tls"

	oidc2 "github.com/trustbloc/wallet/pkg/restapi/common/oidc"
	"github.com/trustbloc/wallet/pkg/restapi/common/store/cookie"
	"github.com/trustbloc/wallet/pkg/restapi/oidc"
	"github.com/trustbloc/wallet/pkg/restapi/wallet"
)

const (
	hostURLFlagName      = "host-url"
	hostURLFlagShorthand = "u"
	hostURLFlagUsage     = "Host Name:Port." +
		" Alternatively, this can be set with the following environment variable: " + hostURLEnvKey
	hostURLEnvKey = "HTTP_SERVER_HOST_URL"

	agentUIURLFlagName  = "agent-ui-url"
	agentUIURLFlagUsage = "Agent UI URL." +
		" Alternatively, this can be set with the following environment variable: " + agentUIURLEnvKey
	agentUIURLEnvKey = "AGENT_UI_URL"

	agentLogLevelFlagName  = "log-level"
	agentLogLevelEnvKey    = "ARIESD_LOG_LEVEL"
	agentLogLevelFlagUsage = "Log level." +
		" Possible values [INFO] [DEBUG] [ERROR] [WARNING] [CRITICAL] . Defaults to INFO if not set." +
		" Alternatively, this can be set with the following environment variable: " + agentLogLevelEnvKey

	tlsCertFileFlagName      = "tls-cert-file"
	tlsCertFileFlagShorthand = "c"
	tlsCertFileFlagUsage     = "tls certificate file." +
		" Alternatively, this can be set with the following environment variable: " + tlsCertFileEnvKey
	tlsCertFileEnvKey = "TLS_CERT_FILE"

	tlsKeyFileFlagName      = "tls-key-file"
	tlsKeyFileFlagShorthand = "k"
	tlsKeyFileFlagUsage     = "tls key file." +
		" Alternatively, this can be set with the following environment variable: " + tlsKeyFileEnvKey
	tlsKeyFileEnvKey = "TLS_KEY_FILE"

	tlsCACertsFlagName  = "tls-cacerts"
	tlsCACertsFlagUsage = "Comma-Separated list of ca certs path." +
		" Alternatively, this can be set with the following environment variable: " + tlsCACertsEnvKey
	tlsCACertsEnvKey = "TLS_CACERTS"

	dependencyMaxRetriesFlagName   = "dep-maxretries"
	dependencyMaxRetriesFlagEnvKey = "HTTP_SERVER_DEP_MAXRETRIES"
	dependencyMaxRetriesFlagUsage  = "Optional. Sets the maximum number of retries while establishing connections with" +
		" external dependencies on startup. Default is 120. Delay between retries is 1s." +
		" Alternatively, this can be set with the following environment variable: " + dependencyMaxRetriesFlagEnvKey
	dependencyMaxRetriesDefault = uint64(120) // nolint:gomnd // false positive ("magic number")

	oidcBasePath    = "/oidc/"
	healthCheckPath = "/healthcheck"
	walletBasePath  = "/wallet/"
)

// Key management config.
const (
	authzKMSURLFlagName  = "authz-kms-url"
	authzKMSURLFlagUsage = "Authorization KMS Server URL"
	authzKMSURLEnvKey    = "HTTP_SERVER_AUTHZ_KMS_URL"

	opsKMSURLFlagName  = "ops-kms-url"
	opsKMSURLFlagUsage = "Operational KMS Server URL"
	opsKMSURLEnvKey    = "HTTP_SERVER_OPS_KMS_URL"

	keyEDVURLFlagName  = "key-edv-url"
	keyEDVURLFlagUsage = "Operational key EDV Server URL"
	keyEDVURLEnvKey    = "HTTP_SERVER_KEY_EDV_URL"
)

// EDV config.
const (
	userEDVURLFlagName  = "user-edv-url"
	userEDVURLFlagUsage = "User EDV Server URL"
	userEDVURLEnvKey    = "HTTP_SERVER_USER_EDV_URL"
)

// Hub auth config.
const (
	hubAuthURLFlagName  = "hub-auth-url"
	hubAuthURLFlagUsage = "Hub Auth Servr URL"
	hubAuthURLEnvKey    = "HTTP_SERVER_HUB_AUTH_URL"
)

// OIDC config.
const (
	oidcProviderURLFlagName  = "oidc-opurl"
	oidcProviderURLFlagUsage = "URL for the OIDC provider." +
		" Alternatively, this can be set with the following environment variable: " + oidcProviderURLEnvKey
	oidcProviderURLEnvKey = "HTTP_SERVER_OIDC_OPURL"

	oidcClientIDFlagName  = "oidc-clientid"
	oidcClientIDFlagUsage = "OAuth2 client_id for OIDC." +
		" Alternatively, this can be set with the following environment variable: " + oidcClientIDEnvKey
	oidcClientIDEnvKey = "HTTP_SERVER_OIDC_CLIENTID"

	oidcClientSecretFlagName  = "oidc-clientsecret" // nolint:gosec // false positive on 'secret'
	oidcClientSecretFlagUsage = "OAuth2 client secret for OIDC." +
		" Alternatively, this can be set with the following environment variable: " + oidcClientSecretEnvKey
	oidcClientSecretEnvKey = "HTTP_SERVER_OIDC_CLIENTSECRET" // nolint:gosec // false positive on 'SECRET'

	// assumed to be the same landing page for all callbacks from all OIDC providers.
	oidcCallbackURLFlagName  = "oidc-callback"
	oidcCallbackURLFlagUsage = "Base URL for the OIDC callback endpoint." +
		" Alternatively, this can be set with the following environment variable: " + oidcCallbackURLEnvKey
	oidcCallbackURLEnvKey = "HTTP_SERVER_OIDC_CALLBACK"
)

// Keys.
const (
	sessionCookieAuthKeyFlagName  = "cookie-auth-key"
	sessionCookieAuthKeyFlagUsage = "Path to the pem-encoded 32-byte key to use to authenticate session cookies." +
		" Alternatively, this can be set with the following environment variable: " + sessionCookieAuthKeyEnvKey
	sessionCookieAuthKeyEnvKey = "HTTP_SERVER_COOKIE_AUTH_KEY"

	sessionCookieEncKeyFlagName  = "cookie-enc-key"
	sessionCookieEncKeyFlagUsage = "Path to the pem-encoded 32-byte key to use to encrypt session cookies." +
		" Alternatively, this can be set with the following environment variable: " + sessionCookieEncKeyEnvKey
	sessionCookieEncKeyEnvKey = "HTTP_SERVER_COOKIE_ENC_KEY"

	sessionCookieMaxAgeFlagName  = "cookie-maxage"
	sessionCookieMaxAgeFlagUsage = "Maximum duration (in seconds) for user session cookies." +
		" Default is 900 (15 minutes)." +
		" Alternatively, this can be set with the following environment variable: " + sessionCookieMaxAgeEnvKey
	sessionCookieMaxAgeEnvKey = "HTTP_SERVER_COOKIE_MAXAGE"
)

var logger = log.New("wallet/wallet-server")

// nolint:gochecknoglobals // this is constant map used only for internal purpose.
var supportedStorageProviders = map[string]func(string, string) (ariesstorage.Provider, error){
	// nolint:unparam // memstorage provider never returns error
	databaseTypeMemOption: func(_, _ string) (ariesstorage.Provider, error) {
		return ariesmem.NewProvider(), nil
	},
	// nolint:unparam // leveldb provider never returns error
	databaseTypeLevelDBOption: func(_, path string) (ariesstorage.Provider, error) {
		return ariesleveldb.NewProvider(path), nil
	},
	databaseTypeCouchDBOption: func(url, prefix string) (ariesstorage.Provider, error) {
		return ariescouchdb.NewProvider(url, ariescouchdb.WithDBPrefix(prefix))
	},
	databaseTypeMYSQLDBOption: func(url, prefix string) (ariesstorage.Provider, error) {
		return ariesmysql.NewProvider(url, ariesmysql.WithDBPrefix(prefix))
	},
	databaseTypeMongoDBOption: func(url, prefix string) (ariesstorage.Provider, error) {
		return ariesmongodb.NewProvider(url, ariesmongodb.WithDBPrefix(prefix))
	},
}

type server interface {
	ListenAndServe(host, certFile, keyFile string, handler http.Handler) error
}

// HTTPServer represents an actual HTTP server implementation.
type HTTPServer struct{}

// ListenAndServe starts the server using the standard Go HTTP server implementation.
func (s *HTTPServer) ListenAndServe(host, certFile, keyFile string, handler http.Handler) error {
	if certFile != "" && keyFile != "" {
		return http.ListenAndServeTLS(host, certFile, keyFile, handler)
	}

	return http.ListenAndServe(host, handler)
}

type httpServerParameters struct {
	dependencyMaxRetries uint64
	srv                  server
	hostURL              string
	tls                  *tlsParameters
	oidc                 *oidcParameters
	cookie               *cookie.Config
	keyServer            *keyServerParameters
	userEDVURL           string
	hubAuthURL           string
	agentUIURL           string
	logLevel             string
	agent                *agentParameters
}

type tlsParameters struct {
	certFile string
	keyFile  string
	config   *tls.Config
}

type oidcParameters struct {
	providerURL  string
	clientID     string
	clientSecret string
	callbackURL  string
}

type keyServerParameters struct {
	authzKMSURL string
	opsKMSURL   string
	keyEDVURL   string
}

// GetStartCmd returns the Cobra start command.
func GetStartCmd(srv server) *cobra.Command {
	startCmd := createStartCmd(srv)

	createFlags(startCmd)

	return startCmd
}

func createStartCmd(srv server) *cobra.Command { //nolint:funlen,gocyclo // no real logic
	return &cobra.Command{
		Use:   "start",
		Short: "Start http server",
		Long:  "Start http server",
		RunE: func(cmd *cobra.Command, args []string) error {
			hostURL, hostURLErr := cmdutils.GetUserSetVarFromString(cmd, hostURLFlagName, hostURLEnvKey, false)
			if hostURLErr != nil {
				return hostURLErr
			}

			agentUIURL, err := cmdutils.GetUserSetVarFromString(cmd, agentUIURLFlagName, agentUIURLEnvKey, false)
			if err != nil {
				return err
			}

			logLevel, err := cmdutils.GetUserSetVarFromString(cmd, agentLogLevelFlagName, agentLogLevelEnvKey, true)
			if err != nil {
				return err
			}

			tlsParams, err := getTLSParams(cmd)
			if err != nil {
				return err
			}

			oidcParams, err := getOIDCParams(cmd)
			if err != nil {
				return err
			}

			retries, err := getDependencyMaxRetries(cmd)
			if err != nil {
				return err
			}

			cookies, err := getCookieParams(cmd)
			if err != nil {
				return err
			}

			keyServer, err := getKeyServerParams(cmd)
			if err != nil {
				return err
			}

			userEDVURL, err := cmdutils.GetUserSetVarFromString(cmd, userEDVURLFlagName, userEDVURLEnvKey, true)
			if err != nil {
				return fmt.Errorf("user edv url : %w", err)
			}

			hubAuthURL, err := cmdutils.GetUserSetVarFromString(cmd, hubAuthURLFlagName, hubAuthURLEnvKey, false)
			if err != nil {
				return fmt.Errorf("hub-auth url : %w", err)
			}

			agentParams, err := getAgentParams(cmd)
			if err != nil {
				return err
			}

			parameters := &httpServerParameters{
				dependencyMaxRetries: retries,
				srv:                  srv,
				hostURL:              hostURL,
				tls:                  tlsParams,
				oidc:                 oidcParams,
				cookie:               cookies,
				keyServer:            keyServer,
				userEDVURL:           userEDVURL,
				hubAuthURL:           hubAuthURL,
				agentUIURL:           agentUIURL,
				logLevel:             logLevel,
				agent:                agentParams,
			}

			return startHTTPServer(parameters)
		},
	}
}

func createFlags(startCmd *cobra.Command) {
	// host url flag
	startCmd.Flags().StringP(hostURLFlagName, hostURLFlagShorthand, "", hostURLFlagUsage)
	// agent ui url flag
	startCmd.Flags().StringP(agentUIURLFlagName, "", "", agentUIURLFlagUsage)
	// agent log level
	startCmd.Flags().StringP(agentLogLevelFlagName, "", "", agentLogLevelFlagUsage)
	startCmd.Flags().StringP(dependencyMaxRetriesFlagName, "", "", dependencyMaxRetriesFlagUsage)
	startCmd.Flags().StringP(authzKMSURLFlagName, "", "", authzKMSURLFlagUsage)
	startCmd.Flags().StringP(opsKMSURLFlagName, "", "", opsKMSURLFlagUsage)
	startCmd.Flags().StringP(keyEDVURLFlagName, "", "", keyEDVURLFlagUsage)
	startCmd.Flags().StringP(userEDVURLFlagName, "", "", userEDVURLFlagUsage)
	startCmd.Flags().StringP(hubAuthURLFlagName, "", "", hubAuthURLFlagUsage)

	createOIDCFlags(startCmd)
	createTLSFlags(startCmd)
	createCookieFlags(startCmd)
	createAgentFlags(startCmd)
}

func createTLSFlags(cmd *cobra.Command) {
	cmd.Flags().StringP(tlsKeyFileFlagName, tlsKeyFileFlagShorthand, "", tlsKeyFileFlagUsage)
	cmd.Flags().StringP(tlsCertFileFlagName, tlsCertFileFlagShorthand, "", tlsCertFileFlagUsage)
	cmd.Flags().StringArrayP(tlsCACertsFlagName, "", []string{}, tlsCACertsFlagUsage)
}

func createOIDCFlags(cmd *cobra.Command) {
	cmd.Flags().StringP(oidcProviderURLFlagName, "", "", oidcProviderURLFlagUsage)
	cmd.Flags().StringP(oidcClientIDFlagName, "", "", oidcClientIDFlagUsage)
	cmd.Flags().StringP(oidcClientSecretFlagName, "", "", oidcClientSecretFlagUsage)
	cmd.Flags().StringP(oidcCallbackURLFlagName, "", "", oidcCallbackURLFlagUsage)
}

func createCookieFlags(cmd *cobra.Command) {
	cmd.Flags().StringP(sessionCookieAuthKeyFlagName, "", "", sessionCookieAuthKeyFlagUsage)
	cmd.Flags().StringP(sessionCookieEncKeyFlagName, "", "", sessionCookieEncKeyFlagUsage)
	cmd.Flags().StringP(sessionCookieMaxAgeFlagName, "", "", sessionCookieMaxAgeFlagUsage)
}

func getDependencyMaxRetries(cmd *cobra.Command) (uint64, error) {
	retriesConfig, err := cmdutils.GetUserSetVarFromString(cmd,
		dependencyMaxRetriesFlagName, dependencyMaxRetriesFlagEnvKey, true)
	if err != nil {
		return 0, fmt.Errorf("failed to configure dependencyMaxRetries: %w", err)
	}

	maxRetries := dependencyMaxRetriesDefault

	if retriesConfig != "" {
		retries, err := strconv.ParseUint(retriesConfig, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("failed to parse dependencyMaxRetries value '%s': %w", retriesConfig, err)
		}

		if retries > 0 {
			maxRetries = retries
		}
	}

	return maxRetries, nil
}

func getTLSParams(cmd *cobra.Command) (*tlsParameters, error) {
	params := &tlsParameters{}

	var err error

	params.certFile, err = cmdutils.GetUserSetVarFromString(cmd, tlsCertFileFlagName, tlsCertFileEnvKey, true)
	if err != nil {
		return nil, fmt.Errorf("failed to configure tls cert file: %w", err)
	}

	params.keyFile, err = cmdutils.GetUserSetVarFromString(cmd, tlsKeyFileFlagName, tlsKeyFileEnvKey, true)
	if err != nil {
		return nil, fmt.Errorf("failed to configure tls key file: %w", err)
	}

	rootCAs, err := cmdutils.GetUserSetVarFromArrayString(cmd, tlsCACertsFlagName, tlsCACertsEnvKey, true)
	if err != nil {
		return nil, fmt.Errorf("failed to configure root CAs: %w", err)
	}

	if len(rootCAs) > 0 {
		certPool, err := tlsutils.GetCertPool(false, rootCAs)
		if err != nil {
			return nil, fmt.Errorf("failed to init tls cert pool: %w", err)
		}

		params.config = &tls.Config{
			RootCAs:    certPool,
			MinVersion: tls.VersionTLS12,
		}
	}

	return params, nil
}

func getOIDCParams(cmd *cobra.Command) (*oidcParameters, error) {
	params := &oidcParameters{}

	var err error

	params.clientID, err = cmdutils.GetUserSetVarFromString(cmd, oidcClientIDFlagName, oidcClientIDEnvKey, false)
	if err != nil {
		return nil, fmt.Errorf("failed to configure OIDC clientID: %w", err)
	}

	params.clientSecret, err = cmdutils.GetUserSetVarFromString(
		cmd, oidcClientSecretFlagName, oidcClientSecretEnvKey, false)
	if err != nil {
		return nil, fmt.Errorf("failed to configure OIDC client secret: %w", err)
	}

	params.callbackURL, err = cmdutils.GetUserSetVarFromString(
		cmd, oidcCallbackURLFlagName, oidcCallbackURLEnvKey, false)
	if err != nil {
		return nil, fmt.Errorf("failed to configure OIDC callback URL: %w", err)
	}

	params.providerURL, err = cmdutils.GetUserSetVarFromString(
		cmd, oidcProviderURLFlagName, oidcProviderURLEnvKey, false)
	if err != nil {
		return nil, fmt.Errorf("failed to configure OIDC provider URL: %w", err)
	}

	return params, nil
}

func getCookieParams(cmd *cobra.Command) (*cookie.Config, error) {
	const defaultMaxAge = 900

	params := &cookie.Config{MaxAge: defaultMaxAge}

	sessionCookieAuthKeyPath, err := cmdutils.GetUserSetVarFromString(cmd,
		sessionCookieAuthKeyFlagName, sessionCookieAuthKeyEnvKey, false)
	if err != nil {
		return nil, fmt.Errorf("failed to configure session cookie auth key: %w", err)
	}

	params.AuthKey, err = parseKey(sessionCookieAuthKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to configure session cookie auth key: %w", err)
	}

	sessionCookieEncKeyPath, err := cmdutils.GetUserSetVarFromString(cmd,
		sessionCookieEncKeyFlagName, sessionCookieEncKeyEnvKey, false)
	if err != nil {
		return nil, fmt.Errorf("failed to configure session cookie enc key: %w", err)
	}

	params.EncKey, err = parseKey(sessionCookieEncKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to configure session cookie enc key: %w", err)
	}

	timeout, err := cmdutils.GetUserSetVarFromString(cmd,
		sessionCookieMaxAgeFlagName, sessionCookieMaxAgeEnvKey, true)
	if err != nil {
		return nil, fmt.Errorf("failed to configure session cookie max age: %w", err)
	}

	if timeout != "" {
		params.MaxAge, err = strconv.Atoi(timeout)
		if err != nil {
			return nil, fmt.Errorf("failed to parse session cookie max age [%s]: %w", timeout, err)
		}
	}

	return params, nil
}

func getKeyServerParams(cmd *cobra.Command) (*keyServerParameters, error) {
	authzKMSURL, err := cmdutils.GetUserSetVarFromString(
		cmd, authzKMSURLFlagName, authzKMSURLEnvKey, false)
	if err != nil {
		return nil, fmt.Errorf("authz key server url : %w", err)
	}

	keyEDVURL, err := cmdutils.GetUserSetVarFromString(
		cmd, keyEDVURLFlagName, keyEDVURLEnvKey, false)
	if err != nil {
		return nil, fmt.Errorf("ops edv server url : %w", err)
	}

	opsKMSURL, err := cmdutils.GetUserSetVarFromString(
		cmd, opsKMSURLFlagName, opsKMSURLEnvKey, false)
	if err != nil {
		return nil, fmt.Errorf("ops key server url : %w", err)
	}

	return &keyServerParameters{
		authzKMSURL: authzKMSURL,
		keyEDVURL:   keyEDVURL,
		opsKMSURL:   opsKMSURL,
	}, nil
}

func parseKey(file string) ([]byte, error) {
	const (
		keyLen = 32
		bitNum = 8
	)

	bits, err := ioutil.ReadFile(filepath.Clean(file))
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", file, err)
	}

	if len(bits) != keyLen {
		return nil, fmt.Errorf("%s: need key of %d bits but got %d", file, keyLen*bitNum, len(bits)*bitNum)
	}

	return bits, nil
}

func initOIDCProvider(providerURL string, retries uint64, tlsConfig *tls.Config) (*oidcp.Provider, error) {
	var provider *oidcp.Provider

	err := backoff.RetryNotify(
		func() error {
			var provErr error
			provider, provErr = oidcp.NewProvider(
				oidcp.ClientContext(
					context.Background(),
					&http.Client{Transport: &http.Transport{
						TLSClientConfig: tlsConfig,
					}},
				),
				providerURL,
			)

			return provErr
		},
		backoff.WithMaxRetries(backoff.NewConstantBackOff(time.Second), retries),
		func(retryErr error, d time.Duration) {
			fmt.Printf(
				"failed to init OIDC provider - will sleep for %s before trying again: %s\n", d, retryErr.Error())
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to init OIDC provider: %w", err)
	}

	return provider, nil
}

func startHTTPServer(parameters *httpServerParameters) error {
	err := setLogLevel(parameters.logLevel)
	if err != nil {
		return fmt.Errorf("failed to set log level: %w", err)
	}

	router, err := router(parameters)
	if err != nil {
		return fmt.Errorf("failed to configure router: %w", err)
	}

	handler := cors.New(
		cors.Options{
			AllowedMethods:   []string{http.MethodGet, http.MethodPost},
			AllowedHeaders:   []string{"Origin", "Accept", "Content-Type", "X-Requested-With", "Authorization"},
			AllowedOrigins:   []string{parameters.agentUIURL},
			AllowCredentials: true,
		},
	).Handler(router)

	logger.Infof("starting http-server on %s...", parameters.hostURL)

	err = parameters.srv.ListenAndServe(
		parameters.hostURL, parameters.tls.certFile, parameters.tls.keyFile,
		handler)
	if err != nil {
		return fmt.Errorf("http server closed unexpectedly: %w", err)
	}

	return err
}

func router(config *httpServerParameters) (http.Handler, error) {
	root := mux.NewRouter()

	root.HandleFunc(healthCheckPath, healthCheckHandler).Methods(http.MethodGet)

	// set message handler
	config.agent.msgHandler = msghandler.NewRegistrar()

	// start agent and get context
	ctx, err := createAriesAgent(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create aries agent: %w", err)
	}

	// OIDC router
	oidcRouter := root.PathPrefix(oidcBasePath).Subrouter()

	err = addOIDCHandlers(oidcRouter, config, ctx.StorageProvider())
	if err != nil {
		return nil, fmt.Errorf("failed to add OIDC handlers: %w", err)
	}

	// wallet agent router
	walletHandlers, err := wallet.GetRESTHandlers(ctx, wallet.WithWebhookURLs(config.agent.webhookURLs...),
		wallet.WithDefaultLabel(config.agent.defaultLabel), wallet.WithMessageHandler(config.agent.msgHandler))
	if err != nil {
		return nil, fmt.Errorf("failed to load wallet handlers: %w", err)
	}

	walletRouter := root.PathPrefix(walletBasePath).Subrouter()

	for _, handler := range walletHandlers {
		walletRouter.HandleFunc(handler.Path(), handler.Handle()).Methods(handler.Method())
	}

	// JSON-LD context operations
	for _, handler := range ldrest.New(ldsvc.New(ctx)).GetRESTHandlers() {
		walletRouter.HandleFunc(handler.Path(), handler.Handle()).Methods(handler.Method())
	}

	return root, nil
}

func addOIDCHandlers(router *mux.Router, config *httpServerParameters, store ariesstorage.Provider) error {
	provider, err := initOIDCProvider(config.oidc.providerURL, config.dependencyMaxRetries, config.tls.config)
	if err != nil {
		return fmt.Errorf("failed to init OIDC provider: %w", err)
	}

	loader, err := createJSONLDDocumentLoader(store)
	if err != nil {
		return fmt.Errorf("create document loader: %w", err)
	}

	oidcOps, err := oidc.New(&oidc.Config{
		WalletDashboard: config.agentUIURL + "/loginhandle",
		TLSConfig:       config.tls.config,
		OIDCClient: oidc2.NewClient(&oidc2.Config{
			TLSConfig:    config.tls.config,
			Provider:     &oidc2.ProviderAdapter{OP: provider, TLSConfig: config.tls.config},
			CallbackURL:  config.oidc.callbackURL,
			ClientID:     config.oidc.clientID,
			ClientSecret: config.oidc.clientSecret,
			Scopes:       []string{oidcp.ScopeOpenID, "profile", "email"},
		}),
		Storage: &oidc.StorageConfig{
			Storage:          store,
			TransientStorage: ariesmem.NewProvider(),
		},
		Cookie: config.cookie,
		KeyServer: &oidc.KeyServerConfig{
			AuthzKMSURL: config.keyServer.authzKMSURL,
			OpsKMSURL:   config.keyServer.opsKMSURL,
			KeyEDVURL:   config.keyServer.keyEDVURL,
		},
		UserEDVURL:   config.userEDVURL,
		HubAuthURL:   config.hubAuthURL,
		JSONLDLoader: loader,
	})
	if err != nil {
		return fmt.Errorf("failed to init oidc ops: %w", err)
	}

	for _, handler := range oidcOps.GetRESTHandlers() {
		router.HandleFunc(handler.Path(), handler.Handle()).Methods(handler.Method())
	}

	return nil
}

type healthCheckResp struct {
	Status      string    `json:"status"`
	CurrentTime time.Time `json:"currentTime"`
}

func healthCheckHandler(rw http.ResponseWriter, _ *http.Request) {
	rw.WriteHeader(http.StatusOK)

	err := json.NewEncoder(rw).Encode(&healthCheckResp{
		Status:      "success",
		CurrentTime: time.Now(),
	})
	if err != nil {
		logger.Errorf("healthcheck response failure, %s", err)
	}
}

func setLogLevel(logLevel string) error {
	if logLevel == "" {
		logLevel = "INFO"
	}

	return setEdgeCoreLogLevel(logLevel)
}

func setEdgeCoreLogLevel(logLevel string) error {
	level, err := log.ParseLevel(logLevel)
	if err != nil {
		return fmt.Errorf("failed to parse log level '%s' : %w", logLevel, err)
	}

	log.SetLevel("", level)

	return nil
}

type ldStoreProvider struct {
	ContextStore        ldstore.ContextStore
	RemoteProviderStore ldstore.RemoteProviderStore
}

func (p *ldStoreProvider) JSONLDContextStore() ldstore.ContextStore {
	return p.ContextStore
}

func (p *ldStoreProvider) JSONLDRemoteProviderStore() ldstore.RemoteProviderStore {
	return p.RemoteProviderStore
}

func createJSONLDDocumentLoader(storageProvider ariesstorage.Provider) (jsonld.DocumentLoader, error) {
	contextStore, err := ldstore.NewContextStore(storageProvider)
	if err != nil {
		return nil, fmt.Errorf("create JSON-LD context store: %w", err)
	}

	remoteProviderStore, err := ldstore.NewRemoteProviderStore(storageProvider)
	if err != nil {
		return nil, fmt.Errorf("create remote provider store: %w", err)
	}

	ldStore := &ldStoreProvider{
		ContextStore:        contextStore,
		RemoteProviderStore: remoteProviderStore,
	}

	documentLoader, err := ld.NewDocumentLoader(ldStore)
	if err != nil {
		return nil, fmt.Errorf("new document loader: %w", err)
	}

	return documentLoader, nil
}
