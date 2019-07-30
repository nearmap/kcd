package handler

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/golang/glog"
	"github.com/eric1313/kcd/history"
	"github.com/eric1313/kcd/resource"
	svc "github.com/eric1313/kcd/service"
	"github.com/pkg/errors"
	goji "goji.io"
	"goji.io/pat"
	"k8s.io/apiserver/pkg/authentication/authenticator"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/authorization/authorizer"
	"k8s.io/apiserver/pkg/authorization/authorizerfactory"
	"k8s.io/apiserver/pkg/server/options"
)

// StaticContentHandler returns a HandlerFunc that writes the given content
// to the response.
func StaticContentHandler(content string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write([]byte(content)); err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
	}
}

// NewServer creates and starts an http server to serve alive and deployment status endpoints
// if server fails to start then, stop channel is closed notifying all listeners to the channel
func NewServer(port int, version string, resourceProvider resource.Provider, historyProvider history.Provider,
	authOptions *options.DelegatingAuthenticationOptions, stopCh chan struct{}) error {

	//authOptions := options.NewDelegatingAuthenticationOptions()
	authenticatorConfig, err := authOptions.ToAuthenticationConfig()
	if err != nil {
		return errors.Wrap(err, "failed to create authenticator config")
	}

	authenticator, _, err := authenticatorConfig.New()
	if err != nil {
		return errors.Wrap(err, "failed to create authenticator")
	}

	authorizerConfig := authorizerfactory.DelegatingAuthorizerConfig{
		AllowCacheTTL: time.Minute * 5,
		DenyCacheTTL:  time.Minute * 5,
	}
	authorizer, err := authorizerConfig.New()
	if err != nil {
		return errors.Wrap(err, "failed to create authorizer")
	}

	mux := goji.NewMux()
	mux.Handle(pat.Get("/alive"), StaticContentHandler("alive"))
	mux.Handle(pat.Get("/version"), StaticContentHandler(version))

	kcdmux := goji.SubMux()
	mux.Handle(pat.New("/kcd/*"), kcdmux)

	kcdmux.Use(accessTokenQueryParam)
	kcdmux.Use(auth(authenticator, authorizer))
	kcdmux.Handle(pat.Get("/v1/resources"), svc.NewAllResourceHandler(resourceProvider))
	kcdmux.Handle(pat.Get("/v1/namespaces/:namespace/resources"), svc.NewResourceHandler(resourceProvider))
	kcdmux.Handle(pat.Post("/v1/namespaces/:namespace/resources/:name"), svc.NewResourceUpdateHandler(resourceProvider))
	kcdmux.Handle(pat.Get("/v1/history/:name"), history.NewHandler(historyProvider))

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 1 * time.Minute,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil {
			if err.Error() != "http: Server closed" {
				glog.V(2).Infof("Server error during ListenAndServe: %v", err)
				close(stopCh)
			}
		}
	}()

	glog.V(1).Infof("Started server on %v", srv.Addr)

	<-stopCh
	glog.V(2).Info("Shutting down http server")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
	glog.V(1).Info("Server gracefully stopped")

	return nil
}

// auth returns middleware that performs authentication and authorization.
func auth(auther authenticator.Request, authzer authorizer.Authorizer) func(http.Handler) http.Handler {
	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userInfo, ok, err := auther.AuthenticateRequest(r)
			if err != nil {
				glog.Errorf("Authentication failed (type=%T): %+v", err, err)
				http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
				return
			}

			if ok {
				if userInfo.GetName() == user.Anonymous {
					ok = false
				}
				groups := userInfo.GetGroups()
				for _, group := range groups {
					if group == user.AllUnauthenticated {
						ok = false
					}
				}
			}

			if !ok {
				glog.V(2).Info("Failed to authenticate user")
				http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
				return
			}

			if glog.V(4) {
				glog.V(4).Infof("Authentication was successful for user %+v", userInfo)
			}

			/* TODO: enable authorization
			atts := authorizer.AttributesRecord{
				User:      user,
				Namespace: "",
				Verb:      "get",
				Resource:  "ContainerVersion",
			}

			authorized, reason, err := authzer.Authorize(atts)
			if err != nil {
				glog.Errorf("Authorization failed: %v", err)
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}
			if authorized != authorizer.DecisionAllow {
				glog.V(1).Infof("Authorization failed (%v) for reason %s", authorized, reason)
				http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
				return
			}
			*/

			h.ServeHTTP(w, r)
		})
	}
}

func accessTokenQueryParam(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if token := q.Get("access_token"); token != "" && r.Header.Get("Authorization") == "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}

		h.ServeHTTP(w, r)
	})
}
