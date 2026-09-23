package contract_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/adk/v2/auth"
)

type toolSnapshot struct {
	Tools []struct {
		Name string `json:"name"`
	} `json:"tools"`
}

func TestEngineToolsMatchSnapshot(t *testing.T) {
	endpoint := os.Getenv("LIKI_ENGINE_MCP_URL")
	if endpoint == "" {
		t.Skip("LIKI_ENGINE_MCP_URL is not configured")
	}
	token := os.Getenv("LIKI_ENGINE_MCP_TOKEN")
	httpClient := &http.Client{Timeout: 10 * time.Second}
	if token != "" {
		httpClient.Transport = &auth.Transport{Provider: auth.StaticToken(token)}
	}
	transport := &mcp.StreamableClientTransport{
		Endpoint:             endpoint,
		HTTPClient:           httpClient,
		MaxRetries:           -1,
		DisableStandaloneSSE: true,
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "liki-agent-contract", Version: "test"}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer session.Close()
	response, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	actual := make([]string, 0, len(response.Tools))
	for _, tool := range response.Tools {
		actual = append(actual, tool.Name)
	}
	sort.Strings(actual)
	snapshotData, err := os.ReadFile("../../contracts/engine-mcp/tools.snapshot.json")
	if err != nil {
		t.Fatalf("read tools snapshot: %v", err)
	}
	var snapshot toolSnapshot
	if err := json.Unmarshal(snapshotData, &snapshot); err != nil {
		t.Fatalf("decode tools snapshot: %v", err)
	}
	expected := make([]string, 0, len(snapshot.Tools))
	for _, tool := range snapshot.Tools {
		expected = append(expected, tool.Name)
	}
	sort.Strings(expected)
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("Engine tools changed: actual=%v expected=%v", actual, expected)
	}
}
