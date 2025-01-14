/*
Copyright SecureKey Technologies Inc. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package oidc // nolint:testpackage // changing to different package requires exposing internal REST handlers

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	ariesmem "github.com/hyperledger/aries-framework-go/component/storageutil/mem"
	"github.com/hyperledger/aries-framework-go/pkg/doc/ld"
	"github.com/hyperledger/aries-framework-go/pkg/doc/signature/jsonld"
	"github.com/hyperledger/aries-framework-go/pkg/doc/signature/suite"
	"github.com/hyperledger/aries-framework-go/pkg/doc/signature/suite/ed25519signature2018"
	"github.com/hyperledger/aries-framework-go/pkg/kms"
	mockldstore "github.com/hyperledger/aries-framework-go/pkg/mock/ld"
	mockstore "github.com/hyperledger/aries-framework-go/pkg/mock/storage"
	ldstore "github.com/hyperledger/aries-framework-go/pkg/store/ld"
	"github.com/stretchr/testify/require"
	"github.com/trustbloc/edge-core/pkg/zcapld"
	"github.com/trustbloc/edv/pkg/client"
	"github.com/trustbloc/edv/pkg/restapi/models"
	"golang.org/x/oauth2"

	oidc2 "github.com/trustbloc/wallet/pkg/restapi/common/oidc"
	"github.com/trustbloc/wallet/pkg/restapi/common/store/cookie"
	"github.com/trustbloc/wallet/pkg/restapi/common/store/tokens"
	"github.com/trustbloc/wallet/pkg/restapi/common/store/user"
)

func TestNew(t *testing.T) {
	t.Run("returns an instance", func(t *testing.T) {
		o, err := New(config(t))
		require.NoError(t, err)
		require.NotNil(t, o)
	})

	t.Run("error if cannot create transient store", func(t *testing.T) {
		expected := errors.New("test")
		config := config(t)
		config.Storage.TransientStorage = &mockstore.MockStoreProvider{
			ErrOpenStoreHandle: expected,
		}
		_, err := New(config)
		require.Error(t, err)
		require.True(t, errors.Is(err, expected))
	})

	t.Run("error if cannot open transient store", func(t *testing.T) {
		expected := errors.New("test")
		config := config(t)
		config.Storage.TransientStorage = &mockstore.MockStoreProvider{
			ErrOpenStoreHandle: expected,
		}
		_, err := New(config)
		require.Error(t, err)
		require.True(t, errors.Is(err, expected))
	})

	t.Run("error if cannot open user store", func(t *testing.T) {
		config := config(t)
		config.Storage.Storage = &mockstore.MockStoreProvider{
			FailNamespace: user.StoreName,
		}
		_, err := New(config)
		require.Error(t, err)
	})

	t.Run("error if cannot open token store", func(t *testing.T) {
		config := config(t)
		config.Storage.Storage = &mockstore.MockStoreProvider{
			FailNamespace: tokens.StoreName,
		}
		_, err := New(config)
		require.Error(t, err)
	})
}

func TestOperation_GetRESTHandlers(t *testing.T) {
	o, err := New(config(t))
	require.NoError(t, err)

	require.NotEmpty(t, o.GetRESTHandlers())
}

func TestOperation_OIDCLoginHandler(t *testing.T) {
	t.Run("redirects to OIDC provider", func(t *testing.T) {
		o, err := New(config(t))
		require.NoError(t, err)
		w := httptest.NewRecorder()
		o.oidcLoginHandler(w, newOIDCLoginRequest())
		require.Equal(t, http.StatusFound, w.Code)
		require.NotEmpty(t, w.Header().Get("Location"))
	})

	t.Run("internal server error if cannot save to cookie store", func(t *testing.T) {
		config := config(t)
		o, err := New(config)
		require.NoError(t, err)
		o.store.cookies = &cookie.MockStore{
			Jar: &cookie.MockJar{
				SaveErr: errors.New("test"),
			},
		}
		w := httptest.NewRecorder()
		o.oidcLoginHandler(w, newOIDCLoginRequest())
		require.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("user already logged in", func(t *testing.T) {
		o, err := New(config(t))
		require.NoError(t, err)
		result := httptest.NewRecorder()
		o.store.cookies = &cookie.MockStore{
			Jar: &cookie.MockJar{
				Cookies: map[interface{}]interface{}{
					userSubCookieName: uuid.New().String(),
				},
			},
		}
		o.oidcLoginHandler(result, newOIDCLoginRequest())
		require.Equal(t, http.StatusMovedPermanently, result.Code)
	})
}

func TestKmsSigner_Sign(t *testing.T) {
	t.Run("failed to sign", func(t *testing.T) {
		_, err := newKMSSigner("", "", "", &kmsHeader{},
			&mockHTTPClient{
				DoFunc: func(req *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusInternalServerError, Body: ioutil.NopCloser(bytes.NewReader([]byte("{}"))),
					}, nil
				},
			}).Sign([]byte("data"))
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to sign from kms")
	})

	t.Run("failed to unmarshal sign resp", func(t *testing.T) {
		_, err := newKMSSigner("", "", "", &kmsHeader{},
			&mockHTTPClient{
				DoFunc: func(req *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK, Body: ioutil.NopCloser(bytes.NewReader([]byte(""))),
					}, nil
				},
			}).Sign([]byte("data"))
		require.Error(t, err)
		require.Contains(t, err.Error(), "unmarshal sign resp")
	})

	t.Run("failed to unmarshal sign resp", func(t *testing.T) {
		_, err := newKMSSigner("", "", "", &kmsHeader{},
			&mockHTTPClient{
				DoFunc: func(req *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK, Body: ioutil.NopCloser(bytes.NewReader([]byte(`{"signature":"1"}`))),
					}, nil
				},
			}).Sign([]byte("data"))
		require.Error(t, err)
		require.Contains(t, err.Error(), "illegal base64 data")
	})
}

func TestOperation_OIDCCallbackHandler(t *testing.T) { //nolint: gocritic,gocognit,gocyclo // test
	const keysPath = "/keys"

	uiEndpoint := "http://test.com/dashboard"

	t.Run("fetches OIDC tokens and redirects to the UI", func(t *testing.T) {
		code := uuid.New().String()
		state := uuid.New().String()

		config := config(t)
		config.WalletDashboard = uiEndpoint
		config.OIDCClient = &oidc2.MockClient{
			OAuthToken: &oauth2.Token{
				AccessToken:  uuid.New().String(),
				RefreshToken: uuid.New().String(),
				TokenType:    "Bearer",
			},
			IDToken: &oidc2.MockClaimer{
				ClaimsFunc: func(i interface{}) error {
					user, ok := i.(*user.User)
					require.True(t, ok)
					user.Sub = uuid.New().String()

					return nil
				},
			},
		}
		config.JSONLDLoader = createTestDocumentLoader(t)

		o, err := New(config)
		require.NoError(t, err)

		o.httpClient = &mockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == authSecretPath {
					return &http.Response{
						StatusCode: http.StatusOK, Body: ioutil.NopCloser(bytes.NewReader([]byte(""))),
					}, nil
				} else if req.URL.Path == authBootstrapDataPath {
					return &http.Response{
						StatusCode: http.StatusOK, Body: ioutil.NopCloser(bytes.NewReader([]byte(""))),
					}, nil
				}

				body := ioutil.NopCloser(bytes.NewReader([]byte("{}")))

				if req.URL.Path == keysPath && req.Method == http.MethodPost {
					body = ioutil.NopCloser(bytes.NewReader(marshal(t, createKeyResp{
						PublicKey: pubEd25519Key(t),
					})))
				}

				return &http.Response{
					StatusCode: http.StatusOK, Body: body,
				}, nil
			},
		}
		o.keyEDVClient = &mockEDVClient{}
		o.userEDVClient = &mockEDVClient{}

		o.store.cookies = &cookie.MockStore{
			Jar: &cookie.MockJar{
				Cookies: map[interface{}]interface{}{
					stateCookieName: state,
				},
			},
		}

		w := httptest.NewRecorder()
		o.oidcCallbackHandler(w, newOIDCCallbackRequest(code, state))
		require.Equal(t, http.StatusFound, w.Code)
		require.Equal(t, uiEndpoint, w.Header().Get("Location"))
	})

	t.Run("error internal server error if cannot fetch the user's session", func(t *testing.T) {
		o, err := New(config(t))
		require.NoError(t, err)
		o.store.cookies = &cookie.MockStore{
			OpenErr: errors.New("test"),
		}
		w := httptest.NewRecorder()
		o.oidcCallbackHandler(w, newOIDCCallbackRequest("code", ""))
		require.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("error bad request if state cookie is not present", func(t *testing.T) {
		o, err := New(config(t))
		require.NoError(t, err)
		w := httptest.NewRecorder()
		o.oidcCallbackHandler(w, newOIDCCallbackRequest("code", ""))
		require.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("error bad request if state query param is missing", func(t *testing.T) {
		o, err := New(config(t))
		require.NoError(t, err)
		o.store.cookies = &cookie.MockStore{
			Jar: &cookie.MockJar{
				Cookies: map[interface{}]interface{}{
					stateCookieName: "123",
				},
			},
		}
		w := httptest.NewRecorder()
		o.oidcCallbackHandler(w, newOIDCCallbackRequest("code", ""))
		require.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("error bad request if state query param does not match state cookie", func(t *testing.T) {
		o, err := New(config(t))
		require.NoError(t, err)
		o.store.cookies = &cookie.MockStore{
			Jar: &cookie.MockJar{
				Cookies: map[interface{}]interface{}{
					stateCookieName: "123",
				},
			},
		}
		w := httptest.NewRecorder()
		o.oidcCallbackHandler(w, newOIDCCallbackRequest("code", "456"))
		require.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("error bad request if code query param is missing", func(t *testing.T) {
		state := uuid.New().String()
		o, err := New(config(t))
		require.NoError(t, err)
		o.store.cookies = &cookie.MockStore{
			Jar: &cookie.MockJar{
				Cookies: map[interface{}]interface{}{
					stateCookieName: state,
				},
			},
		}
		w := httptest.NewRecorder()
		o.oidcCallbackHandler(w, newOIDCCallbackRequest("", state))
		require.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("error internal server error if cannot fetch session cookie", func(t *testing.T) {
		state := uuid.New().String()
		config := config(t)
		o, err := New(config)
		require.NoError(t, err)
		o.store.cookies = &cookie.MockStore{
			OpenErr: errors.New("test"),
		}
		w := httptest.NewRecorder()
		o.oidcCallbackHandler(w, newOIDCCallbackRequest("code", state))
		require.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("error internal server error if cannot persist session cookies", func(t *testing.T) {
		state := uuid.New().String()
		config := config(t)
		o, err := New(config)
		require.NoError(t, err)
		o.store.cookies = &cookie.MockStore{
			Jar: &cookie.MockJar{
				Cookies: map[interface{}]interface{}{
					stateCookieName: state,
				},
				SaveErr: errors.New("test"),
			},
		}
		w := httptest.NewRecorder()
		o.oidcCallbackHandler(w, newOIDCCallbackRequest("code", state))
		require.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("error bad gateway if cannot exchange code for token", func(t *testing.T) {
		state := uuid.New().String()
		config := config(t)
		config.OIDCClient = &oidc2.MockClient{
			OAuthErr: errors.New("test"),
		}
		o, err := New(config)
		require.NoError(t, err)
		o.store.cookies = &cookie.MockStore{
			Jar: &cookie.MockJar{
				Cookies: map[interface{}]interface{}{
					stateCookieName: state,
				},
			},
		}
		w := httptest.NewRecorder()
		o.oidcCallbackHandler(w, newOIDCCallbackRequest("code", state))
		require.Equal(t, http.StatusBadGateway, w.Code)
	})

	t.Run("error bad gateway if cannot verify id_token", func(t *testing.T) {
		state := uuid.New().String()
		config := config(t)
		config.OIDCClient = &oidc2.MockClient{
			IDTokenErr: errors.New("test"),
		}
		o, err := New(config)
		require.NoError(t, err)
		o.store.cookies = &cookie.MockStore{
			Jar: &cookie.MockJar{
				Cookies: map[interface{}]interface{}{
					stateCookieName: state,
				},
			},
		}
		w := httptest.NewRecorder()
		o.oidcCallbackHandler(w, newOIDCCallbackRequest("code", state))
		require.Equal(t, http.StatusBadGateway, w.Code)
	})

	t.Run("error internal server error if cannot parse id_token", func(t *testing.T) {
		state := uuid.New().String()
		config := config(t)
		config.OIDCClient = &oidc2.MockClient{
			IDToken: &oidc2.MockClaimer{
				ClaimsErr: errors.New("test"),
			},
		}
		o, err := New(config)
		require.NoError(t, err)
		o.store.cookies = &cookie.MockStore{
			Jar: &cookie.MockJar{
				Cookies: map[interface{}]interface{}{
					stateCookieName: state,
				},
			},
		}
		w := httptest.NewRecorder()
		o.oidcCallbackHandler(w, newOIDCCallbackRequest("code", state))
		require.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("error internal server error if cannot query user store", func(t *testing.T) {
		userSub := uuid.New().String()
		state := uuid.New().String()
		config := config(t)
		config.Storage.Storage = &mockstore.MockStoreProvider{
			Store: &mockstore.MockStore{
				Store: map[string]mockstore.DBEntry{
					userSub: {Value: []byte(userSub)},
				},
				ErrGet: errors.New("test"),
			},
		}
		config.OIDCClient = &oidc2.MockClient{
			IDToken: &oidc2.MockClaimer{
				ClaimsFunc: func(i interface{}) error {
					user, ok := i.(*user.User)
					require.True(t, ok)
					user.Sub = userSub

					return nil
				},
			},
			OAuthToken: &oauth2.Token{
				AccessToken:  uuid.New().String(),
				RefreshToken: uuid.New().String(),
			},
		}
		o, err := New(config)
		require.NoError(t, err)
		o.store.cookies = &cookie.MockStore{
			Jar: &cookie.MockJar{
				Cookies: map[interface{}]interface{}{
					stateCookieName: state,
				},
			},
		}
		w := httptest.NewRecorder()
		o.oidcCallbackHandler(w, newOIDCCallbackRequest("code", state))
		require.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("error internal server error if cannot save to user store", func(t *testing.T) {
		state := uuid.New().String()
		config := config(t)
		config.Storage.Storage = &mockstore.MockStoreProvider{
			Store: &mockstore.MockStore{
				Store:  make(map[string]mockstore.DBEntry),
				ErrPut: errors.New("test"),
			},
		}
		config.OIDCClient = &oidc2.MockClient{
			OAuthToken: &oauth2.Token{
				AccessToken:  uuid.New().String(),
				RefreshToken: uuid.New().String(),
				TokenType:    "Bearer",
			},
			IDToken: &oidc2.MockClaimer{
				ClaimsFunc: func(i interface{}) error {
					user, ok := i.(*user.User)
					require.True(t, ok)
					user.Sub = uuid.New().String()

					return nil
				},
			},
		}
		o, err := New(config)
		require.NoError(t, err)
		o.store.cookies = &cookie.MockStore{
			Jar: &cookie.MockJar{
				Cookies: map[interface{}]interface{}{
					stateCookieName: state,
				},
			},
		}
		w := httptest.NewRecorder()
		o.oidcCallbackHandler(w, newOIDCCallbackRequest("code", state))
		require.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("failure to create authz keystore", func(t *testing.T) {
		state := uuid.New().String()
		ops := setupOnboardingTest(t, state)
		ops.httpClient = &mockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       ioutil.NopCloser(bytes.NewReader([]byte(""))),
				}, nil
			},
		}

		w := httptest.NewRecorder()
		ops.oidcCallbackHandler(w, newOIDCCallbackRequest(uuid.New().String(), state))

		require.Equal(t, http.StatusInternalServerError, w.Code)
		require.Contains(t, w.Body.String(), "create authz keystore")
	})

	t.Run("failure to create authz key", func(t *testing.T) {
		state := uuid.New().String()
		ops := setupOnboardingTest(t, state)
		ops.httpClient = &mockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == createKeyStorePath {
					return &http.Response{
						StatusCode: http.StatusOK, Body: ioutil.NopCloser(bytes.NewReader([]byte("{}"))),
					}, nil
				}

				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       ioutil.NopCloser(bytes.NewReader([]byte(""))),
				}, nil
			},
		}

		w := httptest.NewRecorder()
		ops.oidcCallbackHandler(w, newOIDCCallbackRequest(uuid.New().String(), state))

		require.Equal(t, http.StatusInternalServerError, w.Code)
		require.Contains(t, w.Body.String(), "create authz key:")
	})

	t.Run("failure to post secret", func(t *testing.T) {
		state := uuid.New().String()
		ops := setupOnboardingTest(t, state)
		ops.httpClient = &mockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusInternalServerError,
					Body:       ioutil.NopCloser(bytes.NewReader([]byte(""))),
				}, nil
			},
		}

		w := httptest.NewRecorder()
		ops.oidcCallbackHandler(w, newOIDCCallbackRequest(uuid.New().String(), state))

		require.Equal(t, http.StatusInternalServerError, w.Code)
		require.Contains(t, w.Body.String(), "post secret share to auth server")
	})

	t.Run("failure to create key data vault", func(t *testing.T) {
		state := uuid.New().String()
		ops := setupOnboardingTest(t, state)
		ops.httpClient = &mockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == authSecretPath {
					return &http.Response{
						StatusCode: http.StatusOK, Body: ioutil.NopCloser(bytes.NewReader([]byte(""))),
					}, nil
				}

				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       ioutil.NopCloser(bytes.NewReader([]byte("{}"))),
				}, nil
			},
		}
		ops.keyEDVClient = &mockEDVClient{
			CreateErr: errors.New("vault creation error"),
		}

		w := httptest.NewRecorder()
		ops.oidcCallbackHandler(w, newOIDCCallbackRequest(uuid.New().String(), state))

		require.Equal(t, http.StatusInternalServerError, w.Code)
		require.Contains(t, w.Body.String(), "vault creation error")
	})

	t.Run("failure to create edv controller", func(t *testing.T) {
		state := uuid.New().String()
		ops := setupOnboardingTest(t, state)
		ops.httpClient = &mockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == authSecretPath {
					return &http.Response{
						StatusCode: http.StatusOK, Body: ioutil.NopCloser(bytes.NewReader([]byte(""))),
					}, nil
				}

				body := ioutil.NopCloser(bytes.NewReader([]byte("{}")))

				if req.URL.Path == createDIDPath {
					body = ioutil.NopCloser(bytes.NewReader([]byte("")))
				}

				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       body,
				}, nil
			},
		}
		ops.keyEDVClient = &mockEDVClient{}

		w := httptest.NewRecorder()
		ops.oidcCallbackHandler(w, newOIDCCallbackRequest(uuid.New().String(), state))

		require.Equal(t, http.StatusInternalServerError, w.Code)
		require.Contains(t, w.Body.String(), "create edv controller")
	})

	t.Run("failure to create chain capability", func(t *testing.T) {
		state := uuid.New().String()
		ops := setupOnboardingTest(t, state)
		ops.httpClient = &mockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == authSecretPath {
					return &http.Response{
						StatusCode: http.StatusOK, Body: ioutil.NopCloser(bytes.NewReader([]byte(""))),
					}, nil
				}

				body := ioutil.NopCloser(bytes.NewReader([]byte("{}")))

				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       body,
				}, nil
			},
		}
		ops.keyEDVClient = &mockEDVClient{Capability: []byte("")}

		w := httptest.NewRecorder()
		ops.oidcCallbackHandler(w, newOIDCCallbackRequest(uuid.New().String(), state))

		require.Equal(t, http.StatusInternalServerError, w.Code)
		require.Contains(t, w.Body.String(), "create chain capability")
	})

	t.Run("failure to create ops keystore", func(t *testing.T) {
		state := uuid.New().String()
		ops := setupOnboardingTest(t, state)
		ops.httpClient = &mockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == authSecretPath {
					return &http.Response{
						StatusCode: http.StatusOK, Body: ioutil.NopCloser(bytes.NewReader([]byte(""))),
					}, nil
				}

				var request createKeyStoreReq

				if req.Body != nil {
					err := json.NewDecoder(req.Body).Decode(&request)
					require.NoError(t, err)

					if request.EDV != nil {
						return &http.Response{
							StatusCode: http.StatusInternalServerError,
							Body:       ioutil.NopCloser(bytes.NewReader([]byte(""))),
						}, nil
					}
				}

				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       ioutil.NopCloser(bytes.NewReader([]byte("{}"))),
				}, nil
			},
		}
		ops.keyEDVClient = &mockEDVClient{}

		w := httptest.NewRecorder()
		ops.oidcCallbackHandler(w, newOIDCCallbackRequest(uuid.New().String(), state))

		require.Equal(t, http.StatusInternalServerError, w.Code)
		require.Contains(t, w.Body.String(), "create operational key store")
	})

	t.Run("failure to create user edv vault", func(t *testing.T) {
		state := uuid.New().String()
		ops := setupOnboardingTest(t, state)
		ops.httpClient = &mockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == authSecretPath {
					return &http.Response{
						StatusCode: http.StatusOK, Body: ioutil.NopCloser(bytes.NewReader([]byte(""))),
					}, nil
				}

				body := ioutil.NopCloser(bytes.NewReader([]byte("{}")))

				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       body,
				}, nil
			},
		}
		ops.keyEDVClient = &mockEDVClient{}
		ops.userEDVClient = &mockEDVClient{CreateErr: errors.New("create error")}

		w := httptest.NewRecorder()
		ops.oidcCallbackHandler(w, newOIDCCallbackRequest(uuid.New().String(), state))

		require.Equal(t, http.StatusInternalServerError, w.Code)
		require.Contains(t, w.Body.String(), "create user edv vault")
	})

	t.Run("create edv ops key failure", func(t *testing.T) {
		state := uuid.New().String()
		ops := setupOnboardingTest(t, state)
		ops.httpClient = &mockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				fmt.Println(req.URL.Path)

				if req.URL.Path == authSecretPath {
					return &http.Response{
						StatusCode: http.StatusOK, Body: ioutil.NopCloser(bytes.NewReader([]byte(""))),
					}, nil
				}

				statusCode := http.StatusOK

				if req.URL.Path == "/v1/keystores//keys" {
					var request createKeyReq

					err := json.NewDecoder(req.Body).Decode(&request)
					require.NoError(t, err)

					if request.KeyType == kms.NISTP256ECDHKW {
						return &http.Response{
							StatusCode: http.StatusInternalServerError,
							Body:       ioutil.NopCloser(bytes.NewReader([]byte(""))),
						}, nil
					}
				}

				body := ioutil.NopCloser(bytes.NewReader([]byte("{}")))

				return &http.Response{
					StatusCode: statusCode,
					Body:       body,
				}, nil
			},
		}
		ops.keyEDVClient = &mockEDVClient{}
		ops.userEDVClient = &mockEDVClient{}

		w := httptest.NewRecorder()
		ops.oidcCallbackHandler(w, newOIDCCallbackRequest(uuid.New().String(), state))

		require.Equal(t, http.StatusInternalServerError, w.Code)
		require.Contains(t, w.Body.String(), "create edv operational key")
	})

	t.Run("create edv hmac key failure", func(t *testing.T) {
		state := uuid.New().String()
		ops := setupOnboardingTest(t, state)
		ops.httpClient = &mockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				fmt.Println(req.URL.Path)

				if req.URL.Path == authSecretPath {
					return &http.Response{
						StatusCode: http.StatusOK, Body: ioutil.NopCloser(bytes.NewReader([]byte(""))),
					}, nil
				}

				statusCode := http.StatusOK

				if req.URL.Path == "/v1/keystores//keys" {
					var request createKeyReq

					err := json.NewDecoder(req.Body).Decode(&request)
					require.NoError(t, err)

					if request.KeyType == kms.HMACSHA256Tag256 {
						return &http.Response{
							StatusCode: http.StatusInternalServerError,
							Body:       ioutil.NopCloser(bytes.NewReader([]byte(""))),
						}, nil
					}
				}

				body := ioutil.NopCloser(bytes.NewReader([]byte("{}")))

				if req.URL.Path == keysPath && req.Method == http.MethodPost {
					body = ioutil.NopCloser(bytes.NewReader(marshal(t, createKeyResp{
						PublicKey: pubEd25519Key(t),
					})))
				}

				return &http.Response{
					StatusCode: statusCode,
					Body:       body,
				}, nil
			},
		}
		ops.keyEDVClient = &mockEDVClient{}
		ops.userEDVClient = &mockEDVClient{}

		w := httptest.NewRecorder()
		ops.oidcCallbackHandler(w, newOIDCCallbackRequest(uuid.New().String(), state))

		require.Equal(t, http.StatusInternalServerError, w.Code)
		require.Contains(t, w.Body.String(), "create edv hmac key")
	})

	t.Run("failure to post bootstrap data", func(t *testing.T) {
		state := uuid.New().String()
		ops := setupOnboardingTest(t, state)
		ops.httpClient = &mockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == authSecretPath {
					return &http.Response{
						StatusCode: http.StatusOK, Body: ioutil.NopCloser(bytes.NewReader([]byte(""))),
					}, nil
				}

				if req.URL.Path == authBootstrapDataPath {
					return &http.Response{
						StatusCode: http.StatusInternalServerError, Body: ioutil.NopCloser(bytes.NewReader([]byte(""))),
					}, nil
				}

				body := ioutil.NopCloser(bytes.NewReader([]byte("{}")))

				if req.URL.Path == keysPath && req.Method == http.MethodPost {
					body = ioutil.NopCloser(bytes.NewReader(marshal(t, createKeyResp{
						PublicKey: pubEd25519Key(t),
					})))
				}

				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       body,
				}, nil
			},
		}
		ops.keyEDVClient = &mockEDVClient{}
		ops.userEDVClient = &mockEDVClient{}

		w := httptest.NewRecorder()
		ops.oidcCallbackHandler(w, newOIDCCallbackRequest(uuid.New().String(), state))

		require.Equal(t, http.StatusInternalServerError, w.Code)
		require.Contains(t, w.Body.String(), "update user bootstrap data")
	})
}

func TestOperation_UserProfileHandler(t *testing.T) {
	t.Run("returns the user profile", func(t *testing.T) {
		sub := uuid.New().String()
		config := config(t)
		config.Storage.Storage = &mockstore.MockStoreProvider{
			Store: &mockstore.MockStore{
				Store: map[string]mockstore.DBEntry{
					sub: {Value: marshal(t, &tokens.UserTokens{})},
				},
			},
		}
		config.OIDCClient = &oidc2.MockClient{
			UserInfoVal: &oidc2.MockClaimer{
				ClaimsFunc: func(v interface{}) error {
					m, ok := v.(*map[string]interface{})
					require.True(t, ok)
					(*m)["sub"] = sub

					return nil
				},
			},
		}
		o, err := New(config)
		require.NoError(t, err)
		o.store.cookies = &cookie.MockStore{
			Jar: &cookie.MockJar{
				Cookies: map[interface{}]interface{}{
					userSubCookieName: sub,
				},
			},
		}

		originalZcap, err := zcapld.NewCapability(&zcapld.Signer{
			SignatureSuite:     ed25519signature2018.New(suite.WithSigner(&mockSigner{})),
			SuiteType:          ed25519signature2018.SignatureType,
			VerificationMethod: "test:123",
			ProcessorOpts:      []jsonld.ProcessorOpts{jsonld.WithDocumentLoader(createTestDocumentLoader(t))},
		}, zcapld.WithParent(uuid.New().URN()))
		require.NoError(t, err)

		originalZcapBytes, err := json.Marshal(originalZcap)
		require.NoError(t, err)

		d := &BootstrapData{
			AuthzKeyStoreURL:  "http://localhost/authz/kms/" + uuid.New().String(),
			OpsKeyStoreURL:    "http://localhost/ops/kms/" + uuid.New().String(),
			UserEDVVaultURL:   "http://localhost/user/vault/" + uuid.New().String(),
			OpsEDVVaultURL:    "http://localhost/ops/vault/" + uuid.New().String(),
			EDVOpsKIDURL:      "http://localhost/ops/kms/" + uuid.New().String() + "/keys/" + uuid.New().String(),
			EDVHMACKIDURL:     "http://localhost/ops/kms/" + uuid.New().String() + "/keys/" + uuid.New().String(),
			UserEDVCapability: string(originalZcapBytes),
		}
		o.httpClient = &mockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				data := userBootstrapData{
					Data: d,
				}

				resp, respErr := json.Marshal(data)
				require.NoError(t, respErr)

				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       ioutil.NopCloser(bytes.NewReader(resp)),
				}, nil
			},
		}

		result := httptest.NewRecorder()
		o.userProfileHandler(result, newUserProfileRequest())
		require.Equal(t, http.StatusOK, result.Code)
		resultData := make(map[string]interface{})
		err = json.NewDecoder(result.Body).Decode(&resultData)
		require.NoError(t, err)
		require.Contains(t, resultData, "sub")
		require.Equal(t, sub, resultData["sub"])

		b, err := json.Marshal(resultData["bootstrap"])
		require.NoError(t, err)

		respData := BootstrapData{}

		err = json.Unmarshal(b, &respData)
		require.NoError(t, err)

		require.Equal(t, d.AuthzKeyStoreURL, respData.AuthzKeyStoreURL)
		require.Equal(t, d.OpsEDVVaultURL, respData.OpsEDVVaultURL)
		require.Equal(t, d.OpsKeyStoreURL, respData.OpsKeyStoreURL)
		require.Equal(t, d.UserEDVVaultURL, respData.UserEDVVaultURL)
		require.Equal(t, d.EDVOpsKIDURL, respData.EDVOpsKIDURL)
		require.Equal(t, d.EDVHMACKIDURL, respData.EDVHMACKIDURL)

		zCapResp := &zcapld.Capability{}

		err = json.Unmarshal([]byte(respData.UserEDVCapability), zCapResp)
		require.NoError(t, err)

		require.Equal(t, originalZcap.Controller, zCapResp.Controller)
		require.Equal(t, originalZcap.ID, zCapResp.ID)
		require.Equal(t, originalZcap.Parent, zCapResp.Parent)
	})

	t.Run("err badrequest if cannot open cookies", func(t *testing.T) {
		o, err := New(config(t))
		require.NoError(t, err)
		o.store.cookies = &cookie.MockStore{
			OpenErr: errors.New("test"),
		}
		result := httptest.NewRecorder()
		o.userProfileHandler(result, newUserProfileRequest())
		require.Equal(t, http.StatusBadRequest, result.Code)
		require.Contains(t, result.Body.String(), "cannot open cookies")
	})

	t.Run("err forbidden if user cookie is not set", func(t *testing.T) {
		o, err := New(config(t))
		require.NoError(t, err)
		result := httptest.NewRecorder()
		o.userProfileHandler(result, newUserProfileRequest())
		require.Equal(t, http.StatusForbidden, result.Code)
		require.Contains(t, result.Body.String(), "not logged in")
	})

	t.Run("err internalservererror if cookie is not a string (should not happen)", func(t *testing.T) {
		o, err := New(config(t))
		require.NoError(t, err)
		o.store.cookies = &cookie.MockStore{
			Jar: &cookie.MockJar{
				Cookies: map[interface{}]interface{}{
					userSubCookieName: struct{}{},
				},
			},
		}
		result := httptest.NewRecorder()
		o.userProfileHandler(result, newUserProfileRequest())
		require.Equal(t, http.StatusInternalServerError, result.Code)
		require.Contains(t, result.Body.String(), "invalid user sub cookie format")
	})

	t.Run("err internal server error if cannot fetch user tokens from storage", func(t *testing.T) {
		config := config(t)
		config.Storage.Storage = &mockstore.MockStoreProvider{
			Store: &mockstore.MockStore{
				ErrGet: errors.New("test"),
			},
		}
		o, err := New(config)
		require.NoError(t, err)
		o.store.cookies = &cookie.MockStore{
			Jar: &cookie.MockJar{
				Cookies: map[interface{}]interface{}{
					userSubCookieName: uuid.New().String(),
				},
			},
		}
		result := httptest.NewRecorder()
		o.userProfileHandler(result, newUserProfileRequest())
		require.Equal(t, http.StatusInternalServerError, result.Code)
		require.Contains(t, result.Body.String(), "failed to fetch user tokens from store")
	})

	t.Run("err badgateway error if cannot fetch userinfo from oidc provider", func(t *testing.T) {
		sub := uuid.New().String()
		config := config(t)
		config.OIDCClient = &oidc2.MockClient{UserInfoErr: errors.New("test")}
		config.Storage.Storage = &mockstore.MockStoreProvider{
			Store: &mockstore.MockStore{
				Store: map[string]mockstore.DBEntry{
					sub: {Value: marshal(t, &tokens.UserTokens{})},
				},
			},
		}
		o, err := New(config)
		require.NoError(t, err)
		o.store.cookies = &cookie.MockStore{
			Jar: &cookie.MockJar{
				Cookies: map[interface{}]interface{}{
					userSubCookieName: sub,
				},
			},
		}
		result := httptest.NewRecorder()
		o.userProfileHandler(result, newUserProfileRequest())
		require.Equal(t, http.StatusBadGateway, result.Code)
		require.Contains(t, result.Body.String(), "failed to fetch user info")
	})

	t.Run("err internalservererror if cannot extract claims from userinfo", func(t *testing.T) {
		sub := uuid.New().String()
		config := config(t)
		config.OIDCClient = &oidc2.MockClient{UserInfoVal: &oidc2.MockClaimer{
			ClaimsErr: errors.New("test"),
		}}
		config.Storage.Storage = &mockstore.MockStoreProvider{
			Store: &mockstore.MockStore{
				Store: map[string]mockstore.DBEntry{
					sub: {Value: marshal(t, &tokens.UserTokens{})},
				},
			},
		}
		o, err := New(config)
		require.NoError(t, err)
		o.store.cookies = &cookie.MockStore{
			Jar: &cookie.MockJar{
				Cookies: map[interface{}]interface{}{
					userSubCookieName: sub,
				},
			},
		}
		result := httptest.NewRecorder()
		o.userProfileHandler(result, newUserProfileRequest())
		require.Equal(t, http.StatusInternalServerError, result.Code)
		require.Contains(t, result.Body.String(), "failed to extract claims from user info")
	})

	t.Run("err internalserver error if cannot fetch temporary bootstrap data", func(t *testing.T) {
		sub := uuid.New().String()
		config := config(t)
		config.Storage.Storage = &mockstore.MockStoreProvider{
			Store: &mockstore.MockStore{
				Store: map[string]mockstore.DBEntry{
					sub: {Value: marshal(t, &tokens.UserTokens{})},
				},
			},
		}
		config.OIDCClient = &oidc2.MockClient{
			UserInfoVal: &oidc2.MockClaimer{
				ClaimsFunc: func(v interface{}) error {
					m, ok := v.(*map[string]interface{})
					require.True(t, ok)
					(*m)["sub"] = sub

					return nil
				},
			},
		}
		o, err := New(config)
		require.NoError(t, err)
		o.store.cookies = &cookie.MockStore{
			Jar: &cookie.MockJar{
				Cookies: map[interface{}]interface{}{
					userSubCookieName: sub,
				},
			},
		}
		o.httpClient = &mockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusInternalServerError,
					Body:       ioutil.NopCloser(bytes.NewReader([]byte("{}"))),
				}, nil
			},
		}

		result := httptest.NewRecorder()
		o.userProfileHandler(result, newUserProfileRequest())
		require.Equal(t, http.StatusInternalServerError, result.Code)
		require.Contains(t, result.Body.String(), "failed to fetch bootstrap data")
	})
}

func TestOperation_UserLogoutHandler(t *testing.T) {
	o, err := New(config(t))
	require.NoError(t, err)
	t.Run("logs out user", func(t *testing.T) {
		o, err = New(config(t))
		require.NoError(t, err)
		o.store.cookies = &cookie.MockStore{
			Jar: &cookie.MockJar{
				Cookies: map[interface{}]interface{}{
					userSubCookieName: uuid.New().String(),
				},
			},
		}
		result := httptest.NewRecorder()
		o.userLogoutHandler(result, newUserLogoutRequest())
		require.Equal(t, http.StatusOK, result.Code)
	})

	t.Run("err badrequest if cannot open cookies", func(t *testing.T) {
		o.store.cookies = &cookie.MockStore{
			OpenErr: errors.New("test"),
		}
		result := httptest.NewRecorder()
		o.userLogoutHandler(result, newUserLogoutRequest())
		require.Equal(t, http.StatusBadRequest, result.Code)
		require.Contains(t, result.Body.String(), "cannot open cookies")
	})
	t.Run("err internal server error if cannot delete cookie", func(t *testing.T) {
		o.store.cookies = &cookie.MockStore{
			Jar: &cookie.MockJar{
				Cookies: map[interface{}]interface{}{
					userSubCookieName: uuid.New().String(),
				},
				SaveErr: errors.New("test"),
			},
		}
		result := httptest.NewRecorder()
		o.userLogoutHandler(result, newUserLogoutRequest())
		require.Equal(t, http.StatusInternalServerError, result.Code)
		require.Contains(t, result.Body.String(), "failed to delete user sub cookie")
	})
	t.Run("no-op if user sub cookie is not found", func(t *testing.T) {
		result := httptest.NewRecorder()
		o.userLogoutHandler(result, newUserLogoutRequest())
		require.Equal(t, http.StatusOK, result.Code)
	})
}

func newOIDCLoginRequest() *http.Request {
	return httptest.NewRequest(http.MethodGet, "/oidc/login", nil)
}

func newOIDCCallbackRequest(code, state string) *http.Request {
	return httptest.NewRequest(http.MethodGet, fmt.Sprintf("/oidc/callback?code=%s&state=%s", code, state), nil)
}

func newUserProfileRequest() *http.Request {
	return httptest.NewRequest(http.MethodGet, "/oidc/userinfo", nil)
}

func newUserLogoutRequest() *http.Request {
	return httptest.NewRequest(http.MethodGet, "/oidc/logout", nil)
}

func config(t *testing.T) *Config {
	t.Helper()

	return &Config{
		OIDCClient: &oidc2.MockClient{},
		Storage: &StorageConfig{
			Storage:          ariesmem.NewProvider(),
			TransientStorage: ariesmem.NewProvider(),
		},
		Cookie: &cookie.Config{
			AuthKey: key(t),
			EncKey:  key(t),
			MaxAge:  900,
		},
		KeyServer: &KeyServerConfig{
			AuthzKMSURL: "",
			KeyEDVURL:   "",
			OpsKMSURL:   "",
		},
		UserEDVURL: "http://example.com",
	}
}

func key(t *testing.T) []byte {
	t.Helper()

	key := make([]byte, 32)

	_, err := rand.Reader.Read(key)
	require.NoError(t, err)

	return key
}

func pubEd25519Key(t *testing.T) []byte {
	t.Helper()

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	return pub
}

func marshal(t *testing.T, v interface{}) []byte {
	t.Helper()

	bits, err := json.Marshal(v)
	require.NoError(t, err)

	return bits
}

func setupOnboardingTest(t *testing.T, state string) *Operation {
	t.Helper()

	config := config(t)
	config.WalletDashboard = "http://test.com/dashboard"
	config.OIDCClient = &oidc2.MockClient{
		OAuthToken: &oauth2.Token{
			AccessToken:  uuid.New().String(),
			RefreshToken: uuid.New().String(),
			TokenType:    "Bearer",
		},
		IDToken: &oidc2.MockClaimer{
			ClaimsFunc: func(i interface{}) error {
				user, ok := i.(*user.User)
				require.True(t, ok)
				user.Sub = uuid.New().String()

				return nil
			},
		},
	}
	config.JSONLDLoader = createTestDocumentLoader(t)

	ops, err := New(config)
	require.NoError(t, err)

	ops.store.cookies = &cookie.MockStore{
		Jar: &cookie.MockJar{
			Cookies: map[interface{}]interface{}{
				stateCookieName: state,
			},
		},
	}

	return ops
}

type mockLDStoreProvider struct {
	ContextStore        ldstore.ContextStore
	RemoteProviderStore ldstore.RemoteProviderStore
}

func (p *mockLDStoreProvider) JSONLDContextStore() ldstore.ContextStore {
	return p.ContextStore
}

func (p *mockLDStoreProvider) JSONLDRemoteProviderStore() ldstore.RemoteProviderStore {
	return p.RemoteProviderStore
}

func createTestDocumentLoader(t *testing.T) *ld.DocumentLoader {
	t.Helper()

	ldStore := &mockLDStoreProvider{
		ContextStore:        mockldstore.NewMockContextStore(),
		RemoteProviderStore: mockldstore.NewMockRemoteProviderStore(),
	}

	loader, err := ld.NewDocumentLoader(ldStore)
	require.NoError(t, err)

	return loader
}

type mockHTTPClient struct {
	DoFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	return m.DoFunc(req)
}

type mockEDVClient struct {
	CreateErr  error
	Capability []byte
}

func (m *mockEDVClient) CreateDataVault(_ *models.DataVaultConfiguration,
	_ ...client.ReqOption) (string, []byte, error) {
	if m.CreateErr != nil {
		return "", nil, m.CreateErr
	}

	ldStore := &mockLDStoreProvider{
		ContextStore:        mockldstore.NewMockContextStore(),
		RemoteProviderStore: mockldstore.NewMockRemoteProviderStore(),
	}

	var zcaps []byte

	if m.Capability != nil {
		zcaps = m.Capability
	} else {
		loader, err := ld.NewDocumentLoader(ldStore)
		if err != nil {
			return "", nil, fmt.Errorf("create document loader: %w", err)
		}

		c, err := zcapld.NewCapability(&zcapld.Signer{
			SignatureSuite:     ed25519signature2018.New(suite.WithSigner(&mockSigner{})),
			SuiteType:          ed25519signature2018.SignatureType,
			VerificationMethod: "test:123",
			ProcessorOpts:      []jsonld.ProcessorOpts{jsonld.WithDocumentLoader(loader)},
		}, zcapld.WithParent(uuid.New().URN()))
		if err != nil {
			return "", nil, err
		}

		b, err := json.Marshal(c)
		if err != nil {
			return "", nil, err
		}

		zcaps = b
	}

	return "http://edv.trustbloc.local" + uuid.New().String(), zcaps, nil
}

type mockSigner struct {
	signVal []byte
	signErr error
}

func (m *mockSigner) Sign(data []byte) ([]byte, error) {
	return m.signVal, m.signErr
}
