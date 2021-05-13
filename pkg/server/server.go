package server

import (
	"fmt"
	"net/http"
	"net/http/pprof"
	"os"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"

	"github.com/replicate/cog/pkg/database"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/serving"
	"github.com/replicate/cog/pkg/storage"
	"github.com/replicate/cog/pkg/util/console"
)

// TODO(andreas): decouple saving zip files from image building into two separate API calls?
// TODO(andreas): separate targets for different CUDA versions? how does that change the yaml design?

const (
	topLevelSourceDir = "source"
)

type Server struct {
	postUploadHooks       []*WebHook
	postBuildHooks        []*WebHook
	postBuildPrimaryHooks []*WebHook
	authDelegate          string
	db                    database.Database
	store                 storage.Storage
	buildQueue            *BuildQueue
}

func NewServer(cpuConcurrency int, gpuConcurrency int, rawPostUploadHooks []string, rawPostBuildHooks []string, rawPostBuildPrimaryHooks []string, authDelegate string, db database.Database, dockerImageBuilder docker.ImageBuilder, servingPlatform serving.Platform, store storage.Storage) (*Server, error) {
	postUploadHooks, err := webHooksFromRaw(rawPostUploadHooks)
	if err != nil {
		return nil, err
	}
	postBuildHooks, err := webHooksFromRaw(rawPostBuildHooks)
	if err != nil {
		return nil, err
	}
	postBuildPrimaryHooks, err := webHooksFromRaw(rawPostBuildPrimaryHooks)
	if err != nil {
		return nil, err
	}
	buildQueue := NewBuildQueue(servingPlatform, dockerImageBuilder, cpuConcurrency, gpuConcurrency)
	return &Server{
		postUploadHooks:       postUploadHooks,
		postBuildHooks:        postBuildHooks,
		postBuildPrimaryHooks: postBuildPrimaryHooks,
		authDelegate:          authDelegate,
		db:                    db,
		store:                 store,
		buildQueue:            buildQueue,
	}, nil
}

func (s *Server) Start(port int) error {
	s.buildQueue.Start()

	router := mux.NewRouter()

	router.Path("/").
		Methods(http.MethodGet).
		HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("OK"))
		})
	router.Path("/ping").
		Methods(http.MethodGet).
		HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("pong"))
		})
	router.Path("/v1/models/{user}/{name}/versions/{id}.zip").
		Methods(http.MethodGet).
		HandlerFunc(s.checkReadAccess(s.DownloadVersion))
	router.Path("/v1/models/{user}/{name}/versions/{id}/files/{path:.+}").
		Methods(http.MethodGet).
		HandlerFunc(s.checkReadAccess(s.DownloadFile))
	router.Path("/v1/models/{user}/{name}/versions/").
		Methods(http.MethodPut).
		HandlerFunc(s.checkWriteAccess(s.ReceiveFile))
	router.Path("/v1/models/{user}/{name}/versions/").
		Methods(http.MethodGet).
		HandlerFunc(s.checkReadAccess(s.ListVersions))
	router.Path("/v1/models/{user}/{name}/versions/{id}").
		Methods(http.MethodGet).
		HandlerFunc(s.checkReadAccess(s.GetVersion))
	router.Path("/v1/models/{user}/{name}/versions/{id}").
		Methods(http.MethodDelete).
		HandlerFunc(s.checkWriteAccess(s.DeleteVersion))
	router.Path("/v1/models/{user}/{name}/cache-hashes/").
		Methods(http.MethodGet).
		HandlerFunc(s.checkReadAccess(s.GetCacheHashes))
	router.Path("/v1/models/{user}/{name}/builds/{id}/logs").
		Methods(http.MethodGet).
		HandlerFunc(s.checkReadAccess(s.SendBuildLogs))
	router.Path("/v1/auth/display-token-url").
		Methods(http.MethodGet).
		HandlerFunc(s.GetDisplayTokenURL)
	router.Path("/v1/auth/verify-token").
		Methods(http.MethodPost).
		HandlerFunc(s.VerifyToken)
	router.Path("/v1/models/{user}/{name}/check-read").
		Methods(http.MethodGet).
		HandlerFunc(s.checkReadAccess(nil))

	if global.ProfilingEnabled {
		router.HandleFunc("/debug/pprof/", pprof.Index)
		router.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		router.HandleFunc("/debug/pprof/profile", pprof.Profile)
		router.HandleFunc("/debug/pprof/profile-mem", profileMemory)
		router.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		router.HandleFunc("/debug/pprof/trace", pprof.Trace)
		router.Handle("/debug/pprof/allocs", pprof.Handler("allocs"))
		router.Handle("/debug/pprof/heap", pprof.Handler("heap"))
		router.Handle("/debug/pprof/goroutine", pprof.Handler("goroutine"))
	}

	console.Infof("Server running on 0.0.0.0:%d", port)

	loggedRouter := handlers.LoggingHandler(os.Stdout, router)

	return http.ListenAndServe(fmt.Sprintf(":%d", port), loggedRouter)
}

func getModelVars(r *http.Request) (user string, name string, id string) {
	vars := mux.Vars(r)
	return vars["user"], vars["name"], vars["id"]
}
