package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/dogestry/dogestry/cli"
	"github.com/dogestry/dogestry/config"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
)

type JSONError struct {
	Detail       JSONErrorDetail `json:"errorDetail"`
	ErrorMessage string          `json:"error"`
}

type JSONErrorDetail struct {
	Message string `json:"message"`
}

type JSONStatus struct {
	Status string `json:"status"`
}

type Server struct {
	ListenAddress string
	TempDir       string
}

func New(listenAddress string, tempDir string) *Server {
	s := &Server{}

	s.ListenAddress = listenAddress
	s.TempDir = tempDir

	return s
}

func errorJSON(msg string) []byte {
	problem := JSONError{
		ErrorMessage: msg,
		Detail: JSONErrorDetail{
			Message: msg,
		},
	}

	// This is how we generate errors. If an error happens here, well...
	bytes, _ := json.Marshal(problem)

	return bytes
}

func statusJSON(msg string) []byte {
	status := struct {
		Status string `json:"status"`
	}{
		Status: msg,
	}

	bytes, _ := json.Marshal(status)

	return bytes
}

func (s *Server) pullHandler(response http.ResponseWriter, req *http.Request) {
	defer req.Body.Close()

	response.Header().Set("Content-Type", "application/json")

	cfg, err := config.NewServerConfig(req.Header.Get("X-Registry-Auth"))
	if err != nil {
		response.Write(errorJSON(err.Error()))
		return
	}

	dogestryCli, err := cli.NewDogestryCli(cfg, make([]string, 0), s.TempDir)
	if err != nil {
		response.Write(errorJSON(err.Error()))
		return
	}

	image := req.URL.Query().Get("fromImage")

	response.Write(statusJSON(fmt.Sprintf("Pulling %s from S3...", image)))

	// Try to flush
	if f, ok := response.(http.Flusher); ok {
		f.Flush()
	}

	quit := make(chan bool)
	go outputWriter(dogestryCli, response, quit)

	if err := dogestryCli.CmdPull(cfg.AWS.S3URL.String(), image); err != nil {
		fmt.Printf("Error pulling image from S3: %v\n", err.Error())
		response.Write(errorJSON("Dogestry server error: " + err.Error()))
		quit <-true
		return
	}

	quit <-true
	response.Write(statusJSON("Done"))
}

func outputWriter(dogestryCli *cli.DogestryCli, response http.ResponseWriter, quitChan chan bool) {
	// Try to grab output if there is any, wait 500ms if not
	for {
		select {
			case msg := <-dogestryCli.OutputChan:
				response.Write(statusJSON(msg))
			case <-quitChan:
				fmt.Println("Finishing request...")
				return
			case <-time.After(500 * time.Millisecond):
		}
	}
}

func (s *Server) healthCheckHandler(response http.ResponseWriter, req *http.Request) {
	defer req.Body.Close()

	response.Write([]byte("OK"))
}

func (s *Server) rootHandler(response http.ResponseWriter, req *http.Request) {
	defer req.Body.Close()

	response.Write(errorJSON("Dogestry API, nothing to see here..."))
}

func (s *Server) ServeHttp() {
	router := mux.NewRouter()

	router.Handle("/{version}/images/create", http.HandlerFunc(s.pullHandler)).Methods("POST")
	router.Handle("/status/check", http.HandlerFunc(s.healthCheckHandler)).Methods("GET")
	router.Handle("/", http.HandlerFunc(s.rootHandler)).Methods("GET")

	http.Handle("/", handlers.LoggingHandler(os.Stdout, router))

	err := http.ListenAndServe(s.ListenAddress, nil)
	if err != nil {
		fmt.Println("Can't start HTTP server: " + err.Error())
		os.Exit(1)
	}
}
