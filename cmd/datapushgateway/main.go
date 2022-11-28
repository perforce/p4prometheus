// This is a companion to prometheus pushgateway
// It is aimed to allow the saving of some arbitrary data specifying customer and instance names
// The aim is to be wrapped by a script which checks in the result on a regular basis.
package main

import (
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/perforce/p4prometheus/version"
	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/alecthomas/kingpin.v2"
	"gopkg.in/yaml.v2"
)

// We extract the bcrypted passwords from the config file for prometheus pushgateway
// A very simple yaml structure.
var usersPasswords = map[string][]byte{}

var logger logrus.Logger

// verifyUserPass verifies that username/password is a valid pair matching
// our userPasswords "database".
func verifyUserPass(username, password string) bool {
	wantPass, hasUser := usersPasswords[username]
	if !hasUser {
		return false
	}
	if cmperr := bcrypt.CompareHashAndPassword(wantPass, []byte(password)); cmperr == nil {
		return true
	}
	return false
}

// basic_auth_users:
//   test_client: $2y$10$nbaHsG/d/LbkBUu4uRLAcuRbhKR/6dti4Wf4/iIDzlGQjspoJe3L2

type AuthFile struct {
	Users map[string]string `yaml:"basic_auth_users"`
}

func readAuthFile(fname string) error {
	yfile, err := os.ReadFile(fname)
	if err != nil {
		log.Fatal(err)
	}

	users := AuthFile{}
	err = yaml.Unmarshal(yfile, &users)
	if err != nil {
		log.Fatal(err)
	}

	for k, v := range users.Users {
		fmt.Printf("%s: %s\n", k, v)
		usersPasswords[k] = []byte(v)
	}
	return nil
}

func saveData(dataDir string, customer string, instance string, data string) error {
	newpath := filepath.Join(dataDir, customer, instance)
	err := os.MkdirAll(newpath, os.ModePerm)
	if err != nil {
		return err
	}
	fname := filepath.Join(newpath, "details.md")
	f, err := os.Create(fname)
	if err != nil {
		logger.Errorf("Error opening %s: %v", fname, err)
		return err
	}
	f.Write([]byte(data))
	err = f.Close()
	if err != nil {
		logger.Errorf("Error closing file: %v", err)
	}
	return nil
}

func main() {
	var (
		authFile = kingpin.Flag(
			"auth.file",
			"Config file for pushgateway specifying user_basic_auth and list of user/bcrypted passwords.",
		).String()
		port = kingpin.Flag(
			"port",
			"Port to listen on.",
		).Default(":9092").String()
		debug = kingpin.Flag(
			"debug",
			"Enable debugging.",
		).Bool()
		dataDir = kingpin.Flag(
			"data",
			"directory where to store uploaded data.",
		).Short('d').Default("data").String()
	)

	kingpin.Version(version.Print("datapushgateway"))
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()

	logger := logrus.New()
	logger.Level = logrus.InfoLevel
	if *debug {
		logger.Level = logrus.DebugLevel
	}

	err := readAuthFile(*authFile)
	if err != nil {
		logger.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/" {
			http.NotFound(w, req)
			return
		}
		w.WriteHeader(200)
		fmt.Fprintf(w, "Data PushGateway\n")
	})

	mux.HandleFunc("/data/", func(w http.ResponseWriter, req *http.Request) {
		user, pass, ok := req.BasicAuth()
		if ok && verifyUserPass(user, pass) {
			fmt.Fprintf(w, "Processed\n")
			query := req.URL.Query()
			logger.Debugf("Request Params: %v", query)
			customer := query.Get("customer")
			instance := query.Get("instance")
			if customer == "" || instance == "" {
				http.Error(w, "Please specify customer and instance", http.StatusBadRequest)
				return
			}
			body, err := io.ReadAll(req.Body)
			if err != nil {
				log.Printf("Error reading body: %v", err)
				http.Error(w, "can't read body", http.StatusBadRequest)
				return
			}
			logger.Debugf("Request Body: %s", string(body))
			saveData(*dataDir, customer, instance, string(body))
			w.Write([]byte("Data saved"))
		} else {
			w.Header().Set("WWW-Authenticate", `Basic realm="api"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
		}
	})

	srv := &http.Server{
		Addr:    *port,
		Handler: mux,
		TLSConfig: &tls.Config{
			MinVersion:               tls.VersionTLS13,
			PreferServerCipherSuites: true,
		},
	}

	log.Printf("Starting server on %s", *port)
	err = srv.ListenAndServe()
	// .ListenAndServeTLS(*certFile, *keyFile)
	log.Fatal(err)
}
