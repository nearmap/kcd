package handler

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"github.com/wish/kcd/events"
	"github.com/wish/kcd/stats"
	"io/ioutil"
	"k8s.io/api/admission/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"net/http"
	"time"

	"github.com/golang/glog"
	"github.com/pkg/errors"
	"github.com/wish/kcd/history"
	"github.com/wish/kcd/resource"
	svc "github.com/wish/kcd/service"
	goji "goji.io"
	"goji.io/pat"
	_ "k8s.io/apimachinery/pkg/runtime"
	_ "k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apiserver/pkg/authentication/authenticator"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/authorization/authorizer"
	"k8s.io/apiserver/pkg/authorization/authorizerfactory"
	"k8s.io/apiserver/pkg/server/options"
)


var (
	runtimeSchema = runtime.NewScheme()
	codecs        = serializer.NewCodecFactory(runtimeSchema)
	deserializer  = codecs.UniversalDeserializer()
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

// VersionPatchHandler returns a HandlerFunc that writes the given content to the response.
func VersionPatchHandler(stats stats.Stats) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		glog.V(4).Info("Enter mutation......")
		var body []byte
		if r.Body != nil {
			if data, err := ioutil.ReadAll(r.Body); err == nil {
				body = data
			}
		}

		var admissionResponse *v1beta1.AdmissionResponse
		ar := v1beta1.AdmissionReview{}
		c := make(chan *v1beta1.AdmissionResponse, 1)
		if _, _, err := deserializer.Decode(body, nil, &ar); err != nil {
			glog.Errorf("Can't decode body: %v", err)
			admissionResponse = &v1beta1.AdmissionResponse{
				Allowed: true,
				Result: &metav1.Status{
					Message: err.Error(),
				},
			}
		} else {
			go func() {
				admissionResponse = events.Mutate(ar.Request, stats)
				c <- admissionResponse
			}()
		}

		admissionResponse = <- c

		admissionReview := v1beta1.AdmissionReview{}
		if admissionResponse != nil {
			admissionReview.Response = admissionResponse
			if ar.Request != nil {
				admissionReview.Response.UID = ar.Request.UID
			}
		}

		if resp, err := json.Marshal(admissionReview); err != nil {
			glog.Errorf("Can't encode response: %v", err)
			http.Error(w, fmt.Sprintf("could not encode response: %v", err), http.StatusInternalServerError)
		} else {
			if _, err := w.Write(resp); err != nil {
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			}
		}
	}
}

// NewServer creates and starts an http server to serve alive and deployment status endpoints
// if server fails to start then, stop channel is closed notifying all listeners to the channel
func NewServer(port int, certFile string, keyFile string, version string, resourceProvider resource.Provider, historyProvider history.Provider,
	authOptions *options.DelegatingAuthenticationOptions, stopCh chan struct{}, stats stats.Stats) error {

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
	mux.Handle(pat.Post("/mutate"), VersionPatchHandler(stats))

	kcdmux := goji.SubMux()
	mux.Handle(pat.New("/kcd/*"), kcdmux)

	kcdmux.Use(accessTokenQueryParam)
	kcdmux.Use(auth(authenticator, authorizer))
	kcdmux.Handle(pat.Get("/v1/resources"), svc.NewAllResourceHandler(resourceProvider))
	kcdmux.Handle(pat.Get("/v1/namespaces/:namespace/resources"), svc.NewResourceHandler(resourceProvider))
	kcdmux.Handle(pat.Post("/v1/namespaces/:namespace/resources/:name"), svc.NewResourceUpdateHandler(resourceProvider))
	kcdmux.Handle(pat.Get("/v1/history/:name"), history.NewHandler(historyProvider))

	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		glog.Errorf("Filed to load key pair: %v", err)
	}

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      mux,
		TLSConfig: 	  &tls.Config{Certificates: []tls.Certificate{pair}},
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 1 * time.Minute,
	}
	go func() {
		if err := srv.ListenAndServeTLS(certFile, keyFile); err != nil {
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
