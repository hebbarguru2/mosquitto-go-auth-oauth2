package main

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
	"os"

	log "github.com/sirupsen/logrus"
)

// init environmental infos
var CI bool

func setupMockOAuthServer() (*httptest.Server, func()) {
	mux := http.NewServeMux()
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")

		if authHeader == "" || authHeader == "Bearer wrong_token" {
			http.Error(w, "Fail", 404)
		}

		// return info to the user in a json
		w.Header().Set("Content-Type", "application/json")

		if authHeader == "Bearer mock_token_normaluser" {
			w.Write([]byte("{\"mqtt\":{\"superuser\":false,\"topics\":{\"read\":[\"/test/topic/read/#\",\"/test/topic/writeread/1\"],\"write\":[\"/test/topic/write/+/db\",\"/test/topic/writeread/1\"]}}}"))
		}

		w.Write([]byte("{\"mqtt\":{\"superuser\":true,\"topics\":{\"read\":[\"/test/topic/read/#\",\"/test/topic/writeread/1\"],\"write\":[\"/test/topic/write/+/db\",\"/test/topic/writeread/1\"]}}}"))
	})

	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		password := r.Form.Get("password")
		username := r.Form.Get("username")
		refreshToken := r.Form.Get("refresh_token")
		grantType := r.Form.Get("grant_type")

		if password == "wrong_password" {
			http.Error(w, "Fail", 404)
			return
		}

		if refreshToken != "" {
			log.Infof("Got refresh request with token %s and grant_type %s.", refreshToken, grantType)
		}

		// normal user register
		if (username == "test_normaluser" && password == "test_normaluser") || refreshToken == "mock_refresh_token" {
			// Should return acccess token back to the user
			w.Header().Set("Content-Type", "application/x-www-form-urlencoded")
			w.Write([]byte("access_token=mock_token_normaluser&scope=user&token_type=bearer&refresh_token=mock_refresh_token&expires_in=0"))
			return
		}

		// superuser register
		if (username == "test_superuser" && password == "test_superuser") || refreshToken == "mock_refresh_token_superuser" {
			w.Header().Set("Content-Type", "application/x-www-form-urlencoded")
			w.Write([]byte("access_token=mock_token_superuser&scope=user&token_type=bearer&refresh_token=mock_refresh_token_superuser&expires_in=0"))
			return
		}
	
		http.Error(w, "Wrong credentials", 404)
	})

	server := httptest.NewServer(mux)

	return server, func() {
		server.Close()
		log.Infof("Close Testserver")
	}
}

func createOAuthServer(t *testing.T, duration int) (*httptest.Server, func()) {
	// start muck server
	server, closeServer := setupMockOAuthServer()
	log.Infof("Start Testserver on location %s", server.URL)

	var authOpts map[string]string
	authOpts = make(map[string]string)

	authOpts["oauth_client_id"] = "clientId"
	authOpts["oauth_client_secret"] = "clientSecret"
	authOpts["oauth_token_url"] = server.URL + "/token"
	authOpts["oauth_userinfo_url"] = server.URL + "/userinfo"
	authOpts["oauth_cache_duration"] = strconv.Itoa(duration)

	err := Init(authOpts, log.InfoLevel)
	if err != nil {
		t.Errorf("Failed to init plugin: %s", err)
	}
	return server, closeServer
}

func TestInit(t *testing.T) {
	CI = (os.Getenv("DRONE") == "true")
	log.Infof("Run the test in the ci: %t", CI)

	_, closeServer := createOAuthServer(t, 0)
	defer closeServer()
}

func TestGetUserPositiv(t *testing.T) {
	// first init plugin to create oauth server and client
	_, closeServer := createOAuthServer(t, 0)
	defer closeServer()

	allowed := GetUser("test_superuser", "test_superuser")
	if !allowed {
		t.Errorf("Positive GetUser() Response was negative!")
	}
}

func TestGetUserNegativ(t *testing.T) {
	_, closeServer := createOAuthServer(t, 0)
	defer closeServer()

	allowed := GetUser("wrong_user", "wrong_password")
	if allowed {
		t.Errorf("Negative GetUser() Response was positive!")
	}
}

func TestGetUserWithTokenPositiv(t *testing.T) {
	_, closeServer := createOAuthServer(t, 0)
	defer closeServer()

	allowed := GetUser("mock_token_superuser", "")
	if !allowed {
		t.Errorf("Positiv GetUser() Response with token was negative!")
	}

	superuser := GetSuperuser("mock_token_superuser")
	if !superuser {
		t.Errorf("Positiv GetSuperuser() Response with token was negative!")
	}
}

func TestGetUserWithTokenNegative(t *testing.T) {
	_, closeServer := createOAuthServer(t, 0)
	defer closeServer()

	allowed := GetUser("wrong_token", "")
	if allowed {
		t.Errorf("Negative GetUser() Response with token was positive!")
	}
}

func TestGetSuperuserPositiv(t *testing.T) {
	// first init plugin to create oauth server and client
	_, closeServer := createOAuthServer(t, 0)
	defer closeServer()

	GetUser("test_superuser", "test_superuser")
	allowed := GetSuperuser("test_superuser")
	if !allowed {
		t.Errorf("Positive GetSuperuser() Response was negative!")
	}
}

func TestGetSuperuserNegativ(t *testing.T) {
	// first init plugin to create oauth server and client
	_, closeServer := createOAuthServer(t, 0)
	defer closeServer()

	GetUser("test_normaluser", "test")
	allowed := GetSuperuser("test_normaluser")
	if allowed {
		t.Errorf("Negative GetSuperuser() Response was positive!")
	}
}

func TestCheckAclPositiv(t *testing.T) {
	// first init plugin to create oauth server and client
	_, closeServer := createOAuthServer(t, 0)
	defer closeServer()

	GetUser("test_superuser", "test_superuser")
	// test read access
	allowed := CheckAcl("test_superuser", "/test/topic/read/sensor", "foo", 1)
	if !allowed {
		t.Errorf("Positive CheckAcl() Response was negative!")
	}

	// test write access
	allowed = CheckAcl("test_superuser", "/test/topic/write/influx/db", "foo", 2)
	if !allowed {
		t.Errorf("Positive CheckAcl() Response was negative!")
	}

	// test write access
	allowed = CheckAcl("test_superuser", "/test/topic/writeread/1", "foo", 3)
	if !allowed {
		t.Errorf("Positive CheckAcl() Response was negative!")
	}
}

func TestCheckAclNegative(t *testing.T) {
	// first init plugin to create oauth server and client
	_, closeServer := createOAuthServer(t, 0)
	defer closeServer()

	GetUser("test_superuser", "test_superuser")
	// test read access
	allowed := CheckAcl("test_superuser", "/test/wrong_topic/read/sensor", "foo", 1)
	if allowed {
		t.Errorf("Negative CheckAcl() Response was positive!")
	}

	// test write access
	allowed = CheckAcl("test_superuser", "/test/wrong_topic/write/influx/db", "foo", 2)
	if allowed {
		t.Errorf("Negative CheckAcl() Response was negative!")
	}

	// test write access
	allowed = CheckAcl("test_superuser", "/test/wrong_topic/writeread/1", "foo", 3)
	if allowed {
		t.Errorf("Negative CheckAcl() Response was negative!")
	}
}

func TestGetUserinfoFromCache(t *testing.T) {
	// first init plugin to create oauth server and client
	_, closeServer := createOAuthServer(t, 10)
	defer closeServer()

	GetUser("test_superuser", "test_superuser")

	// first request should get info from backend
	GetSuperuser("test_superuser")

	// second from cache
	allowed := GetSuperuser("test_superuser")
	if !allowed {
		t.Errorf("Test cache check was positive")
	}
}

func TestRefreshExpiredAccessTokenCredentials(t *testing.T) {
	// hard to test. when set the expired_at in the response to a
	// short time, the client call the refresh request instant
	// but nobody like long tests so the test only runs on ci
	if CI {
		// first init plugin to create oauth server and client
		_, closeServer := createOAuthServer(t, 0)
		defer closeServer()

		GetUser("test_superuser", "test_superuser")


		time.Sleep(65 * time.Second)

		// second try after expired
		allowed := GetSuperuser("test_superuser")
		if !allowed {
			t.Errorf("Test cache check was positive")
		}
	}
}

func TestRefreshExpiredAccessTokenWithoutCrediantials(t *testing.T) {
	// hard to test. when set the expired_at in the response to a
	// short time, the client call the refresh request instant
	// but nobody like long tests so the test only runs on ci
	if CI {
		// first init plugin to create oauth server and client
		_, closeServer := createOAuthServer(t, 0)
		defer closeServer()

		GetUser("mock_token_superuser", "")


		time.Sleep(65 * time.Second)

		// second try after expired
		allowed := GetSuperuser("mock_token_superuser")
		if !allowed {
			t.Errorf("Test cache check was positive")
		}
	}
}


