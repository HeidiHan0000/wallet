/*
Copyright SecureKey Technologies Inc. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package startcmd

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/hyperledger/aries-framework-go-ext/component/vdr/orb"
	"github.com/hyperledger/aries-framework-go/pkg/controller/command"
	"github.com/hyperledger/aries-framework-go/pkg/didcomm/transport"
	arieshttp "github.com/hyperledger/aries-framework-go/pkg/didcomm/transport/http"
	"github.com/hyperledger/aries-framework-go/pkg/didcomm/transport/ws"
	"github.com/hyperledger/aries-framework-go/pkg/framework/aries"
	"github.com/hyperledger/aries-framework-go/pkg/framework/aries/api/vdr"
	"github.com/hyperledger/aries-framework-go/pkg/framework/aries/defaults"
	"github.com/hyperledger/aries-framework-go/pkg/framework/context"
	"github.com/hyperledger/aries-framework-go/pkg/vdr/httpbinding"
	ariesstorage "github.com/hyperledger/aries-framework-go/spi/storage"
	"github.com/spf13/cobra"
	cmdutils "github.com/trustbloc/edge-core/pkg/utils/cmd"
)

const (
	// api token flag.
	agentTokenFlagName      = "api-token"
	agentTokenEnvKey        = "ARIESD_API_TOKEN" // nolint:gosec //This is just a token ENV variable name
	agentTokenFlagShorthand = "t"
	agentTokenFlagUsage     = "Check for bearer token in the authorization header (optional)." +
		" Alternatively, this can be set with the following environment variable: " + agentTokenEnvKey

	databaseTypeFlagName      = "database-type"
	databaseTypeEnvKey        = "ARIESD_DATABASE_TYPE"
	databaseTypeFlagShorthand = "q"
	databaseTypeFlagUsage     = "The type of database to use for everything except key storage. " +
		"Supported options: mem, couchdb, mysql, leveldb, mongodb. " +
		" Alternatively, this can be set with the following environment variable: " + databaseTypeEnvKey

	databaseURLFlagName      = "database-url"
	databaseURLEnvKey        = "ARIESD_DATABASE_URL"
	databaseURLFlagShorthand = "v"
	databaseURLFlagUsage     = "The URL of the database. Not needed if using memstore." +
		" For CouchDB, include the username:password@ text if required. " +
		" Alternatively, this can be set with the following environment variable: " + databaseURLEnvKey

	databasePrefixFlagName      = "database-prefix"
	databasePrefixEnvKey        = "ARIESD_DATABASE_PREFIX"
	databasePrefixFlagShorthand = "p"
	databasePrefixFlagUsage     = "An optional prefix to be used when creating and retrieving underlying databases. " +
		" Alternatively, this can be set with the following environment variable: " + databasePrefixEnvKey

	databaseTimeoutFlagName  = "database-timeout"
	databaseTimeoutFlagUsage = "Total time in seconds to wait until the db is available before giving up." +
		" Default: " + databaseTimeoutDefault + " seconds." +
		" Alternatively, this can be set with the following environment variable: " + databaseTimeoutEnvKey
	databaseTimeoutEnvKey  = "ARIESD_DATABASE_TIMEOUT"
	databaseTimeoutDefault = "30"

	// webhook url flag.
	agentWebhookFlagName      = "webhook-url"
	agentWebhookEnvKey        = "ARIESD_WEBHOOK_URL"
	agentWebhookFlagShorthand = "w"
	agentWebhookFlagUsage     = "URL to send notifications to." +
		" This flag can be repeated, allowing for multiple listeners." +
		" Alternatively, this can be set with the following environment variable (in CSV format): " + agentWebhookEnvKey

	// default label flag.
	agentDefaultLabelFlagName      = "agent-default-label"
	agentDefaultLabelEnvKey        = "ARIESD_DEFAULT_LABEL"
	agentDefaultLabelFlagShorthand = "l"
	agentDefaultLabelFlagUsage     = "Default Label for this agent. Defaults to blank if not set." +
		" Alternatively, this can be set with the following environment variable: " + agentDefaultLabelEnvKey

	// http resolver url flag.
	agentHTTPResolverFlagName      = "http-resolver-url"
	agentHTTPResolverEnvKey        = "ARIESD_HTTP_RESOLVER"
	agentHTTPResolverFlagShorthand = "r"
	agentHTTPResolverFlagUsage     = "HTTP binding DID resolver method and url. Values should be in `method@url` format." +
		" This flag can be repeated, allowing multiple http resolvers. Defaults to peer DID resolver if not set." +
		" Alternatively, this can be set with the following environment variable (in CSV format): " +
		agentHTTPResolverEnvKey

	// trustbloc domain url flag.
	agentTrustblocDomainFlagName      = "trustbloc-domain"
	agentTrustblocDomainEnvKey        = "ARIESD_TRUSTBLOC_DOMAIN"
	agentTrustblocDomainFlagShorthand = "d"
	agentTrustblocDomainFlagUsage     = "Trustbloc domain URL." +
		" Alternatively, this can be set with the following environment variable (in CSV format): " +
		agentTrustblocDomainEnvKey

	// trustbloc resolver url flag.
	agentTrustblocResolverFlagName  = "trustbloc-resolver"
	agentTrustblocResolverEnvKey    = "ARIESD_TRUSTBLOC_RESOLVER"
	agentTrustblocResolverFlagUsage = "Trustbloc resolver URL." +
		" Alternatively, this can be set with the following environment variable (in CSV format): " +
		agentTrustblocResolverEnvKey

	// outbound transport flag.
	agentOutboundTransportFlagName      = "outbound-transport"
	agentOutboundTransportEnvKey        = "ARIESD_OUTBOUND_TRANSPORT"
	agentOutboundTransportFlagShorthand = "o"
	agentOutboundTransportFlagUsage     = "Outbound transport type." +
		" This flag can be repeated, allowing for multiple transports." +
		" Possible values [http] [ws]. Defaults to http if not set." +
		" Alternatively, this can be set with the following environment variable: " + agentOutboundTransportEnvKey

	// inbound host url flag.
	agentInboundHostFlagName      = "inbound-host"
	agentInboundHostEnvKey        = "ARIESD_INBOUND_HOST"
	agentInboundHostFlagShorthand = "i"
	agentInboundHostFlagUsage     = "Inbound Host Name:Port. This is used internally to start the inbound server." +
		" Values should be in `scheme@url` format." +
		" This flag can be repeated, allowing to configure multiple inbound transports." +
		" Alternatively, this can be set with the following environment variable: " + agentInboundHostEnvKey

	// inbound host external url flag.
	agentInboundHostExternalFlagName      = "inbound-host-external"
	agentInboundHostExternalEnvKey        = "ARIESD_INBOUND_HOST_EXTERNAL"
	agentInboundHostExternalFlagShorthand = "e"
	agentInboundHostExternalFlagUsage     = "Inbound Host External Name:Port and values should be in `scheme@url` format" +
		" This is the URL for the inbound server as seen externally." +
		" If not provided, then the internal inbound host will be used here." +
		" This flag can be repeated, allowing to configure multiple inbound transports." +
		" Alternatively, this can be set with the following environment variable: " + agentInboundHostExternalEnvKey

	// transport return route option flag.
	agentTransportReturnRouteFlagName  = "transport-return-route"
	agentTransportReturnRouteEnvKey    = "ARIESD_TRANSPORT_RETURN_ROUTE"
	agentTransportReturnRouteFlagUsage = "Transport Return Route option." +
		" Refer https://github.com/hyperledger/aries-framework-go/blob/8449c727c7c44f47ed7c9f10f35f0cd051dcb4e9/" +
		"pkg/framework/aries/framework.go#L165-L168." +
		" Alternatively, this can be set with the following environment variable: " + agentTransportReturnRouteEnvKey

	// websocket read limit flag.
	agentWebSocketReadLimitFlagName  = "web-socket-read-limit"
	agentWebSocketReadLimitEnvKey    = "ARIESD_WEB_SOCKET_READ_LIMIT"
	agentWebSocketReadLimitFlagUsage = "WebSocket read limit sets the custom max number of bytes to" +
		" read for a single message when WebSocket transport is used. Defaults to 32KB." +
		" Alternatively, this can be set with the following environment variable: " + agentWebSocketReadLimitEnvKey

	// remote JSON-LD context provider url flag.
	agentContextProviderFlagName  = "context-provider-url"
	agentContextProviderEnvKey    = "ARIESD_CONTEXT_PROVIDER_URL"
	agentContextProviderFlagUsage = "Remote context provider URL to get JSON-LD contexts from." +
		" This flag can be repeated, allowing setting up multiple context providers." +
		" Alternatively, this can be set with the following environment variable (in CSV format): " +
		agentContextProviderEnvKey

	httpProtocol      = "http"
	websocketProtocol = "ws"

	databaseTypeMemOption     = "mem"
	databaseTypeCouchDBOption = "couchdb"
	databaseTypeMYSQLDBOption = "mysql"
	databaseTypeLevelDBOption = "leveldb"
	databaseTypeMongoDBOption = "mongodb"
)

// agentParameters contains parameters for wallet server agent.
type agentParameters struct {
	defaultLabel         string
	transportReturnRoute string
	token                string
	trustblocDomain      string
	trustblocResolver    string
	webhookURLs          []string
	httpResolvers        []string
	outboundTransports   []string
	inboundHostInternals []string
	inboundHostExternals []string
	contextProviderURLs  []string
	msgHandler           command.MessageHandler
	dbParam              *dbParam
	websocketReadLimit   int64
}

type dbParam struct {
	dbType  string
	url     string
	prefix  string
	timeout uint64
}

//nolint:funlen,gocyclo // breaking down will make it look like complex logic.
func getAgentParams(cmd *cobra.Command) (*agentParameters, error) {
	token, err := cmdutils.GetUserSetVarFromString(cmd, agentTokenFlagName, agentTokenEnvKey, true)
	if err != nil {
		return nil, err
	}

	inboundHosts, err := cmdutils.GetUserSetVarFromArrayString(cmd, agentInboundHostFlagName, agentInboundHostEnvKey, true)
	if err != nil {
		return nil, err
	}

	inboundHostExternals, err := cmdutils.GetUserSetVarFromArrayString(cmd, agentInboundHostExternalFlagName,
		agentInboundHostExternalEnvKey, true)
	if err != nil {
		return nil, err
	}

	dbParam, err := getDBParam(cmd)
	if err != nil {
		return nil, err
	}

	defaultLabel, err := cmdutils.GetUserSetVarFromString(cmd, agentDefaultLabelFlagName,
		agentDefaultLabelEnvKey, true)
	if err != nil {
		return nil, err
	}

	webhookURLs, err := cmdutils.GetUserSetVarFromArrayString(cmd, agentWebhookFlagName,
		agentWebhookEnvKey, true)
	if err != nil {
		return nil, err
	}

	httpResolvers, err := cmdutils.GetUserSetVarFromArrayString(cmd, agentHTTPResolverFlagName,
		agentHTTPResolverEnvKey, true)
	if err != nil {
		return nil, err
	}

	trustblocDomain, err := cmdutils.GetUserSetVarFromString(cmd, agentTrustblocDomainFlagName,
		agentTrustblocDomainEnvKey, true)
	if err != nil {
		return nil, err
	}

	trustblocResolver, err := cmdutils.GetUserSetVarFromString(cmd, agentTrustblocResolverFlagName,
		agentTrustblocResolverEnvKey, true)
	if err != nil {
		return nil, err
	}

	outboundTransports, err := cmdutils.GetUserSetVarFromArrayString(cmd, agentOutboundTransportFlagName,
		agentOutboundTransportEnvKey, true)
	if err != nil {
		return nil, err
	}

	transportReturnRoute, err := cmdutils.GetUserSetVarFromString(cmd, agentTransportReturnRouteFlagName,
		agentTransportReturnRouteEnvKey, true)
	if err != nil {
		return nil, err
	}

	contextProviderURLs, err := cmdutils.GetUserSetVarFromArrayString(cmd, agentContextProviderFlagName,
		agentContextProviderEnvKey, true)
	if err != nil {
		return nil, err
	}

	websocketReadLimit, err := getWebSocketReadLimit(cmd)
	if err != nil {
		return nil, err
	}

	return &agentParameters{
		token:                token,
		inboundHostInternals: inboundHosts,
		inboundHostExternals: inboundHostExternals,
		dbParam:              dbParam,
		defaultLabel:         defaultLabel,
		webhookURLs:          webhookURLs,
		httpResolvers:        httpResolvers,
		trustblocDomain:      trustblocDomain,
		trustblocResolver:    trustblocResolver,
		outboundTransports:   outboundTransports,
		transportReturnRoute: transportReturnRoute,
		contextProviderURLs:  contextProviderURLs,
		websocketReadLimit:   websocketReadLimit,
	}, nil
}

func getDBParam(cmd *cobra.Command) (*dbParam, error) {
	dbParam := &dbParam{}

	var err error

	dbParam.dbType, err = cmdutils.GetUserSetVarFromString(cmd, databaseTypeFlagName, databaseTypeEnvKey, false)
	if err != nil {
		return nil, err
	}

	dbParam.url, err = cmdutils.GetUserSetVarFromString(cmd, databaseURLFlagName, databaseURLEnvKey, true)
	if err != nil {
		return nil, err
	}

	dbParam.prefix, err = cmdutils.GetUserSetVarFromString(cmd, databasePrefixFlagName, databasePrefixEnvKey, true)
	if err != nil {
		return nil, err
	}

	dbTimeout, err := cmdutils.GetUserSetVarFromString(cmd, databaseTimeoutFlagName, databaseTimeoutEnvKey, true)
	if err != nil {
		return nil, err
	}

	if dbTimeout == "" || dbTimeout == "0" {
		dbTimeout = databaseTimeoutDefault
	}

	t, err := strconv.Atoi(dbTimeout)
	if err != nil {
		return nil, fmt.Errorf("failed to parse db timeout %s: %w", dbTimeout, err)
	}

	dbParam.timeout = uint64(t)

	return dbParam, nil
}

func getWebSocketReadLimit(cmd *cobra.Command) (int64, error) {
	readLimitVal, err := cmdutils.GetUserSetVarFromString(cmd, agentWebSocketReadLimitFlagName,
		agentWebSocketReadLimitEnvKey, true)
	if err != nil {
		return 0, err
	}

	var readLimit int64

	if readLimitVal != "" {
		readLimit, err = strconv.ParseInt(readLimitVal, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("failed to parse web socket read limit %s: %w", readLimitVal, err)
		}
	}

	return readLimit, nil
}

func createAgentFlags(cmd *cobra.Command) {
	// agent token flag
	cmd.Flags().StringP(agentTokenFlagName, agentTokenFlagShorthand, "", agentTokenFlagUsage)

	// inbound host flag
	cmd.Flags().StringSliceP(agentInboundHostFlagName, agentInboundHostFlagShorthand, []string{},
		agentInboundHostFlagUsage)

	// inbound external host flag
	cmd.Flags().StringSliceP(agentInboundHostExternalFlagName, agentInboundHostExternalFlagShorthand,
		[]string{}, agentInboundHostExternalFlagUsage)

	// db type
	cmd.Flags().StringP(databaseTypeFlagName, databaseTypeFlagShorthand, "", databaseTypeFlagUsage)

	// db url
	cmd.Flags().StringP(databaseURLFlagName, databaseURLFlagShorthand, "", databaseURLFlagUsage)

	// db prefix
	cmd.Flags().StringP(databasePrefixFlagName, databasePrefixFlagShorthand, "", databasePrefixFlagUsage)

	// webhook url flag
	cmd.Flags().StringSliceP(agentWebhookFlagName, agentWebhookFlagShorthand, []string{}, agentWebhookFlagUsage)

	// http resolver url flag
	cmd.Flags().StringSliceP(agentHTTPResolverFlagName, agentHTTPResolverFlagShorthand, []string{},
		agentHTTPResolverFlagUsage)

	// trustbloc domain url flag
	cmd.Flags().StringP(agentTrustblocDomainFlagName, agentTrustblocDomainFlagShorthand, "",
		agentTrustblocDomainFlagUsage)

	// trustbloc resolver url flag
	cmd.Flags().StringP(agentTrustblocResolverFlagName, "", "", agentTrustblocResolverFlagUsage)

	// agent default label flag
	cmd.Flags().StringP(agentDefaultLabelFlagName, agentDefaultLabelFlagShorthand, "",
		agentDefaultLabelFlagUsage)

	// agent outbound transport flag
	cmd.Flags().StringSliceP(agentOutboundTransportFlagName, agentOutboundTransportFlagShorthand, []string{},
		agentOutboundTransportFlagUsage)

	// transport return route option flag
	cmd.Flags().StringP(agentTransportReturnRouteFlagName, "", "", agentTransportReturnRouteFlagUsage)

	// db timeout
	cmd.Flags().StringP(databaseTimeoutFlagName, "", "", databaseTimeoutFlagUsage)

	// remote JSON-LD context provider url flag
	cmd.Flags().StringSliceP(agentContextProviderFlagName, "", []string{}, agentContextProviderFlagUsage)

	// websocket read limit flag
	cmd.Flags().StringP(agentWebSocketReadLimitFlagName, "", "", agentWebSocketReadLimitFlagUsage)
}

func createStoreProviders(params *dbParam) (ariesstorage.Provider, error) {
	provider, supported := supportedStorageProviders[params.dbType]
	if !supported {
		return nil, fmt.Errorf("key database type not set to a valid type." +
			" run start --help to see the available options")
	}

	var store ariesstorage.Provider

	err := backoff.RetryNotify(
		func() error {
			var openErr error
			store, openErr = provider(params.url, params.prefix)

			return openErr
		},
		backoff.WithMaxRetries(backoff.NewConstantBackOff(time.Second), params.timeout),
		func(retryErr error, t time.Duration) {
			logger.Warnf(
				"failed to connect to storage, will sleep for %s before trying again : %s\n",
				t, retryErr)
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to storage at %s: %w", params.url, err)
	}

	logger.Infof("ariesstore init - connected to storage at %s", params.url)

	return store, nil
}

func createAriesAgent(parameters *httpServerParameters) (*context.Provider, error) { //nolint:funlen //ignore
	agentParams := parameters.agent

	var opts []aries.Option

	storePro, err := createStoreProviders(agentParams.dbParam)
	if err != nil {
		return nil, err
	}

	opts = append(opts, aries.WithStoreProvider(storePro))

	if agentParams.transportReturnRoute != "" {
		opts = append(opts, aries.WithTransportReturnRoute(agentParams.transportReturnRoute))
	}

	inboundTransportOpt, err := getInboundTransportOpts(agentParams.inboundHostInternals,
		agentParams.inboundHostExternals, parameters.tls.certFile, parameters.tls.keyFile,
		agentParams.websocketReadLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to start aries agent rest on port [%s], failed to inbound tranpsort opt : %w",
			parameters.hostURL, err)
	}

	opts = append(opts, inboundTransportOpt...)

	VDRs, err := createVDRs(agentParams.httpResolvers, agentParams.trustblocDomain)
	if err != nil {
		return nil, err
	}

	for i := range VDRs {
		opts = append(opts, aries.WithVDR(VDRs[i]))
	}

	outboundTransportOpts, err := getOutboundTransportOpts(agentParams.outboundTransports,
		agentParams.websocketReadLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to start aries agent rest on port [%s], failed to outbound transport opts : %w",
			parameters.hostURL, err)
	}

	opts = append(opts, outboundTransportOpts...)
	opts = append(opts, aries.WithMessageServiceProvider(agentParams.msgHandler))

	if len(agentParams.contextProviderURLs) > 0 {
		opts = append(opts, aries.WithJSONLDContextProviderURL(agentParams.contextProviderURLs...))
	}

	framework, err := aries.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to start aries agent rest on port [%s], failed to initialize framework :  %w",
			parameters.hostURL, err)
	}

	ctx, err := framework.Context()
	if err != nil {
		return nil, fmt.Errorf("failed to start aries agent rest on port [%s], failed to get aries context : %w",
			parameters.hostURL, err)
	}

	return ctx, nil
}

func getInboundTransportOpts(inboundHostInternals, inboundHostExternals []string, certFile,
	keyFile string, websocketReadLimit int64) ([]aries.Option, error) {
	internalHost, err := getInboundSchemeToURLMap(inboundHostInternals)
	if err != nil {
		return nil, fmt.Errorf("inbound internal host : %w", err)
	}

	externalHost, err := getInboundSchemeToURLMap(inboundHostExternals)
	if err != nil {
		return nil, fmt.Errorf("inbound external host : %w", err)
	}

	var opts []aries.Option

	for scheme, host := range internalHost {
		switch scheme {
		case httpProtocol:
			opts = append(opts, defaults.WithInboundHTTPAddr(host, externalHost[scheme], certFile, keyFile))
		case websocketProtocol:
			opts = append(opts, defaults.WithInboundWSAddr(host, externalHost[scheme], certFile, keyFile,
				websocketReadLimit))
		default:
			return nil, fmt.Errorf("inbound transport [%s] not supported", scheme)
		}
	}

	return opts, nil
}

func getInboundSchemeToURLMap(schemeHostStr []string) (map[string]string, error) {
	const validSliceLen = 2

	schemeHostMap := make(map[string]string)

	for _, schemeHost := range schemeHostStr {
		schemeHostSlice := strings.Split(schemeHost, "@")
		if len(schemeHostSlice) != validSliceLen {
			return nil, fmt.Errorf("invalid inbound host option: Use scheme@url to pass the option")
		}

		schemeHostMap[schemeHostSlice[0]] = schemeHostSlice[1]
	}

	return schemeHostMap, nil
}

func createVDRs(resolvers []string, trustblocDomain string) ([]vdr.VDR, error) {
	const numPartsResolverOption = 2
	// set maps resolver to its methods
	// e.g the set of ["trustbloc@http://resolver.com", "v1@http://resolver.com"] will be
	// {"http://resolver.com": {"trustbloc":{}, "v1":{} }}
	set := make(map[string]map[string]struct{})
	// order maps URL to its initial index
	order := make(map[string]int)

	idx := -1

	for _, resolver := range resolvers {
		r := strings.Split(resolver, "@")
		if len(r) != numPartsResolverOption {
			return nil, fmt.Errorf("invalid http resolver options found: %s", resolver)
		}

		if set[r[1]] == nil {
			set[r[1]] = map[string]struct{}{}
			idx++
		}

		order[r[1]] = idx

		set[r[1]][r[0]] = struct{}{}
	}

	VDRs := make([]vdr.VDR, len(set), len(set)+1)

	for url := range set {
		methods := set[url]

		resolverVDR, err := httpbinding.New(url, httpbinding.WithAccept(func(method string) bool {
			_, ok := methods[method]

			return ok
		}))
		if err != nil {
			return nil, fmt.Errorf("failed to create new universal resolver vdr: %w", err)
		}

		VDRs[order[url]] = resolverVDR
	}

	blocVDR, err := orb.New(nil,
		orb.WithDomain(trustblocDomain))
	if err != nil {
		return nil, err
	}

	VDRs = append(VDRs, blocVDR)

	return VDRs, nil
}

func getOutboundTransportOpts(outboundTransports []string, websocketReadLimit int64) ([]aries.Option, error) {
	var opts []aries.Option

	var transports []transport.OutboundTransport

	for _, outboundTransport := range outboundTransports {
		switch outboundTransport {
		case httpProtocol:
			outbound, err := arieshttp.NewOutbound(arieshttp.WithOutboundHTTPClient(&http.Client{}))
			if err != nil {
				return nil, fmt.Errorf("http outbound transport initialization failed: %w", err)
			}

			transports = append(transports, outbound)
		case websocketProtocol:
			var outboundOpts []ws.OutboundClientOpt

			if websocketReadLimit > 0 {
				outboundOpts = append(outboundOpts, ws.WithOutboundReadLimit(websocketReadLimit))
			}

			transports = append(transports, ws.NewOutbound(outboundOpts...))
		default:
			return nil, fmt.Errorf("outbound transport [%s] not supported", outboundTransport)
		}
	}

	if len(transports) > 0 {
		opts = append(opts, aries.WithOutboundTransports(transports...))
	}

	return opts, nil
}
