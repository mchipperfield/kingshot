package main

import (
	"reflect"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestParseActiveCodes(t *testing.T) {
	t.Parallel()

	got := parseActiveCodes(" CODE1, ,CODE2 ,  CODE3  ,")
	want := []string{"CODE1", "CODE2", "CODE3"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseActiveCodes() = %v, want %v", got, want)
	}
}

func TestCommandNameSet(t *testing.T) {
	t.Parallel()

	commands := []*discordgo.ApplicationCommand{
		{Name: "register"},
		{Name: "code"},
	}

	names := commandNameSet(commands)

	if _, ok := names["register"]; !ok {
		t.Fatalf("expected register command in set")
	}
	if _, ok := names["code"]; !ok {
		t.Fatalf("expected code command in set")
	}
	if len(names) != 2 {
		t.Fatalf("expected 2 command names, got %d", len(names))
	}
}
