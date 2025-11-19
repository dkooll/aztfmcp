package main

import (
	"context"
	"flag"
	"log"
	"os"

	"github.com/dkooll/aztfmcp/pkg/mcp"
)

func main() {
	org := flag.String("org", "hashicorp", "GitHub organization name")
	repo := flag.String("repo", "terraform-provider-azurerm", "GitHub repository to index")
	token := flag.String("token", "", "GitHub personal access token (optional, for higher rate limits)")
	dbPath := flag.String("db", "azurerm-provider.db", "Path to SQLite database file")
	flag.Parse()

	log.SetOutput(os.Stderr)
	log.Println("Starting AzureRM Provider MCP Server")
	log.Printf("Repository: %s/%s", *org, *repo)
	log.Printf("Database will be initialized at: %s (on first sync)", *dbPath)

	server := mcp.NewServer(*dbPath, *token, *org, *repo)
	if err := server.Run(context.Background(), os.Stdin, os.Stdout); err != nil {
		log.Printf("Server stopped: %v", err)
	}
}
