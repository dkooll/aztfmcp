package util

import (
	"slices"
	"testing"
)

func TestExpandQueryVariants(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect []string
	}{
		{"empty", "   ", []string{""}},
		{"simple", "VirtualNetwork", []string{"virtualnetwork"}},
		{"hyphen", "virtual-network", []string{"virtual network", "virtualnetwork"}},
		{"mixed", "virtual_network/test", []string{"virtual network test", "virtualnetworktest"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExpandQueryVariants(tt.input)
			for _, want := range tt.expect {
				if !slices.Contains(got, want) {
					t.Fatalf("expected %q in variants %v", want, got)
				}
			}
		})
	}
}

func TestExtractProvider(t *testing.T) {
	if got := ExtractProvider("azurerm_resource"); got != "azurerm" {
		t.Fatalf("expected azurerm, got %s", got)
	}
	if got := ExtractProvider(""); got != "" {
		t.Fatalf("expected empty string for empty input, got %s", got)
	}
}
