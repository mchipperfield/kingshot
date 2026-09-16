package main

import (
	"reflect"
	"testing"
	"time"
)

func TestWatchGatewayTimesOutAfterDisconnect(t *testing.T) {
	status := make(chan bool)
	unhealthy := watchGateway(status, 10*time.Millisecond)

	status <- false
	select {
	case <-unhealthy:
	case <-time.After(time.Second):
		t.Fatal("watchGateway did not report a prolonged disconnect")
	}
}

func TestWatchGatewayCancelsTimeoutAfterReconnect(t *testing.T) {
	status := make(chan bool)
	unhealthy := watchGateway(status, 20*time.Millisecond)

	status <- false
	status <- true
	select {
	case <-unhealthy:
		t.Fatal("watchGateway reported an unhealthy gateway after reconnect")
	case <-time.After(40 * time.Millisecond):
	}

	status <- false
	<-unhealthy
}

func TestParseActiveCodes(t *testing.T) {
	t.Parallel()

	got := parseActiveCodes(" CODE1, ,CODE2 ,  CODE3  ,")
	want := []string{"CODE1", "CODE2", "CODE3"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseActiveCodes() = %v, want %v", got, want)
	}
}

func TestValidateConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		token            string
		firestoreProject string
		wantErr          string
	}{
		{name: "missing bot token", firestoreProject: "project", wantErr: "bot_token is required"},
		{name: "missing Firestore project", token: "token", wantErr: "firestore_project_id is required"},
		{name: "valid", token: "token", firestoreProject: "project"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfig(tt.token, tt.firestoreProject)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateConfig() error = %v", err)
				}
				return
			}
			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("validateConfig() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}
