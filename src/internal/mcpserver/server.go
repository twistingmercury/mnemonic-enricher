package mcpserver

import (
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rs/zerolog"

	"github.com/twistingmercury/mnemonic/internal/config"
	"github.com/twistingmercury/mnemonic/internal/version"
)

// NewMCPServer creates a configured MCP server with all 3 pattern search
// tools registered. The returned server is ready to be wrapped in an HTTP
// handler via NewMCPHTTPHandler.
func NewMCPServer(deps ToolDependencies, logger zerolog.Logger, mcpCfg config.MCPConfig) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "mnemonic",
		Version: version.Version(),
	}, &mcp.ServerOptions{
		SchemaCache: mcp.NewSchemaCache(),
	})

	RegisterTools(server, deps, logger, mcpCfg)

	return server
}

// RegisterTools registers all 3 pattern search tools on the MCP server.
// The deps parameter provides access to services needed by tool handlers.
func RegisterTools(server *mcp.Server, deps ToolDependencies, logger zerolog.Logger, mcpCfg config.MCPConfig) {
	mcp.AddTool(server, searchPatternsTool, handleSearchPatterns(deps, logger, mcpCfg.DefaultSearchThreshold))
	mcp.AddTool(server, findRelatedPatternsTool, handleFindRelatedPatterns(deps, logger))
	mcp.AddTool(server, getPatternTool, handleGetPattern(deps, logger))
}

// NewMCPHTTPHandler creates a StreamableHTTPHandler wrapping the MCP server.
// The handler implements http.Handler and serves JSON-RPC 2.0 over HTTP POST.
// Stateless mode is enabled because all tools are stateless.
func NewMCPHTTPHandler(server *mcp.Server) *mcp.StreamableHTTPHandler {
	return mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
		return server
	}, &mcp.StreamableHTTPOptions{
		Stateless: true,
	})
}

// NewMCPHTTPServer creates the net/http.Server for the MCP endpoint.
// The caller is responsible for starting and stopping the server
// (typically via errgroup).
func NewMCPHTTPServer(mcpCfg config.MCPConfig, host string, mcpHandler http.Handler) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("/mcp", mcpHandler)

	return &http.Server{
		Addr:         mcpCfg.Address(host),
		Handler:      mux,
		ReadTimeout:  mcpCfg.ReadTimeout,
		WriteTimeout: mcpCfg.WriteTimeout,
		IdleTimeout:  mcpCfg.IdleTimeout,
	}
}
