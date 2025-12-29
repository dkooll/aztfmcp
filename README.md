# aztfmcp

An MCP (Model Context Protocol) server that indexes, analyzes, and serves the terraform provider azurerm go source code on demand to MCP compatible AI agents.

## Features

**Provider Discovery**

List and search all AzureRM resources and data sources with fast, FTS-backed lookups

**Schema Inspection**

Deep schema analysis showing attributes, types, validations, ForceNew flags, conflicts, and nested blocks

**Code Search**

Search across all provider Go files for patterns, functions, or free text

**Resource Comparison**

Compare two resources side-by-side to understand schema differences and similarities

**Update Behavior Analysis**

Determine whether changing a specific attribute requires resource recreation or supports in-place updates

**Breaking Change Explanations**

Understand why attributes are marked ForceNew and get migration strategies

**Similar Resource Discovery**

Find resources with similar schemas using ML-based Jaccard similarity scoring

**Validation Analysis**

Detect missing or weak validations in resource schemas and get improvement suggestions

**Dependency Tracing**

Visualize attribute dependencies including ConflictsWith, ExactlyOneOf, AtLeastOneOf, and RequiredWith

**Source Code Access**

Retrieve Go source snippets including schema definitions, CustomizeDiff logic, timeouts, and importers

**Documentation Access**

Fetch official provider documentation for any resource or data source

**Test Discovery**

List acceptance tests that cover specific resources

**GitHub Sync**

Syncs and indexes the provider from GitHub into a local SQLite database for fast queries.

Supports incremental updates and extracts schema metadata from Go AST parsing.

**Release Tracking**

Track and categorize CHANGELOG entries by type (new resources, enhancements, bug fixes, dependency updates, deprecations).

Parse release metadata including new list resources, action resources, and ephemeral resources.

Backfill specific releases on demand for historical analysis.

**Service Organization**

Organize resources by Azure service (Compute, Network, Storage, etc.) with automatic linking.

Track service metadata including GitHub labels and documentation categories.

Filter and search resources by service affiliation.

**API Version Tracking**

Track Azure API versions for each resource from go-azure-sdk imports.

Identify resources using outdated API versions.

Compare API versions across similar resources.

## Prerequisites

Go 1.23.0 or later

SQLite (with FTS5 support - included in most modern installations)

GitHub Personal Access Token (optional, for higher rate limits) with `repo → public_repo` rights.

## Configuration

**Server flags**

The server accepts command-line flags for configuration:

--org - GitHub organization name (default: "hashicorp")

--repo - Repository name (default: "terraform-provider-azurerm")

--token - GitHub personal access token (optional; improves rate limits)

--db - Path to SQLite database file (default: "azurerm-provider.db")

**Adding to AI agents**

To use this MCP server with AI agents (Claude CLI, Copilot, Codex CLI, or other MCP-compatible clients), add it to their configuration file:

```json
{
  "mcpServers": {
    "aztfmcp": {
      "command": "/path/to/aztfmcp",
      "args": ["--org", "hashicorp", "--repo", "terraform-provider-azurerm", "--token", "YOUR_TOKEN"]
    }
  }
}
```

## Build from source

make build

## Example Prompts

**Once configured, you can ask natural language questions. All data is pre-indexed and available instantly:**

**Update Behavior & Breaking Changes**

Does changing `address_space` on `azurerm_virtual_network` require recreation?

Why does `dns_prefix` on `azurerm_kubernetes_cluster` force a new resource?

If I change the SKU on `azurerm_storage_account`, will it recreate or update in-place?

**Resource Comparison & Discovery**

Compare `azurerm_linux_virtual_machine` and `azurerm_windows_virtual_machine` schemas

Find resources similar to `azurerm_linux_virtual_machine`

What attributes do `azurerm_app_service` and `azurerm_function_app` have in common?

**Schema Deep Dive**

Show me all ForceNew attributes on `azurerm_virtual_network`

Which attributes on `azurerm_storage_account` are marked sensitive?

List all nested blocks in `azurerm_kubernetes_cluster`

**Validation Analysis**

What validations are missing on `azurerm_storage_account`?

Does `azurerm_key_vault` have proper name format validation?

Check `azurerm_network_security_rule` for weak port validations

**Dependency Tracing**

What does `key_vault_key_id` within the `customer_managed_key` block on `azurerm_storage_account` conflict with?

Show all dependencies for `kubelet_identity` block in `azurerm_kubernetes_cluster`

Trace `ExactlyOneOf` constraints on `azurerm_storage_account`

**Provider Source Inspection**

Show the CustomizeDiff logic for `azurerm_cdn_profile`

What are the timeout configurations for `azurerm_kubernetes_cluster`?

Get the importer snippet for `azurerm_storage_account`

**Search & Discovery**

Find all resources using `suppress.CaseDifference`

Which resources have more than 8 ForceNew attributes?

Search for resources with file path containing 'services/network' and filter by data_source kind and show the full list

**Releases & Versioning**

Summarize the latest provider release

Show me what changed in `azurerm_windows_web_app` in version 4.52.0

What new resources were added in the last release?

Query the indexed release entries for new_list_resource type from the last 3 releases.

**Service Organization**

Which Azure service category (Compute, Network, Storage, etc.) has the most resource types? db file is located at ...

Show Compute resources with API version 2024-03-01 or newer

**API Version Tracking**

What API version does `azurerm_windows_virtual_machine` use?

Find resources using outdated API versions (before 2024)

Compare API versions between `azurerm_linux_virtual_machine` and `azurerm_windows_virtual_machine`

**Testing & Documentation**

Show acceptance tests for `azurerm_kubernetes_cluster`

Get the Example Usage section from `azurerm_virtual_network` docs

Find test files for `azurerm_storage_account` related to file shares

**Sync and Maintenance**

Run a full provider sync

Sync updates provider

## Tips

When inspecting schemas, ask for a compact view to get concise bullet lists instead of detailed tables. You can also filter by specific flags like ForceNew, required, or sensitive attributes to focus on what matters.

For finding similar resources, you can adjust how strict the matching is - start with broader matches around 15-30% similarity since Azure resources tend to be quite diverse even within the same service area. Most resources show less than 30% similarity due to Azure's varied schema designs.

When comparing two resources, you'll see which attributes they share and which are unique to each. Even closely related resources may have low similarity scores, which is normal given how Azure organizes resource properties.

For understanding why changes force recreation, you'll get explanations covering the ForceNew flag, technical reasons, and practical migration strategies to help plan your updates.

## Notes

GitHub token is optional; without it, syncing still works but may hit lower API rate limits. Pass `--token` to raise limits.

Initial full sync takes ~20 seconds and indexes 9,000+ Go files. Subsequent incremental syncs are much faster.

For large queries in agent prompts, include the SQLite database location so the agent can query it in the working directory, or the path passed via `--db`).

Deleting the database file will cause a full rebuild the next time the server is called.

The parser extracts schema metadata using Go AST analysis for accuracy.

Release summaries maintain the most recent 40 versions by default; older tags can be backfilled on demand when needed.

## Direct Database Access

The indexed data is stored in a SQLite database file with FTS5 enabled. You can query it directly for ad‑hoc inspection:

**Basic Resource Queries**

`sqlite3 azurerm-provider.db "SELECT name, kind FROM provider_resources LIMIT 10"`

`sqlite3 azurerm-provider.db "SELECT name FROM provider_resources WHERE name LIKE '%network%' AND kind = 'resource'"`

`sqlite3 azurerm-provider.db "
  SELECT r.name, COUNT(a.id) as force_new_count
  FROM provider_resources r
  JOIN provider_resource_attributes a ON r.id = a.resource_id
  WHERE a.force_new = 1 AND r.kind = 'resource'
  GROUP BY r.id
  ORDER BY force_new_count DESC
  LIMIT 10"
`

`sqlite3 azurerm-provider.db "
  SELECT r.name, a.name as attribute
  FROM provider_resources r
  JOIN provider_resource_attributes a ON r.id = a.resource_id
  WHERE a.sensitive = 1
  ORDER BY r.name"
`

**Service Organization Queries**

`sqlite3 azurerm-provider.db "
  SELECT ps.name, COUNT(pr.id) as resource_count
  FROM provider_services ps
  JOIN provider_resources pr ON ps.id = pr.service_id
  GROUP BY ps.id
  ORDER BY resource_count DESC
  LIMIT 10"
`

`sqlite3 azurerm-provider.db "
  SELECT pr.name, ps.name as service, ps.github_label
  FROM provider_resources pr
  JOIN provider_services ps ON pr.service_id = ps.id
  WHERE pr.name LIKE '%kubernetes%'"
`

**API Version Queries**

`sqlite3 azurerm-provider.db "
  SELECT name, api_version
  FROM provider_resources
  WHERE api_version IS NOT NULL
  ORDER BY api_version DESC
  LIMIT 10"
`

`sqlite3 azurerm-provider.db "
  SELECT api_version, COUNT(*) as count
  FROM provider_resources
  WHERE api_version IS NOT NULL
  GROUP BY api_version
  ORDER BY count DESC"
`

**Release & Change Type Queries**

`sqlite3 azurerm-provider.db "
  SELECT change_type, COUNT(*) as count
  FROM provider_release_entries
  WHERE change_type IS NOT NULL
  GROUP BY change_type
  ORDER BY count DESC"
`

`sqlite3 azurerm-provider.db "
  SELECT pr.version, pr.release_date, COUNT(pe.id) as entry_count
  FROM provider_releases pr
  LEFT JOIN provider_release_entries pe ON pr.id = pe.release_id
  GROUP BY pr.id
  ORDER BY pr.release_date DESC
  LIMIT 5"
`

`sqlite3 azurerm-provider.db "
  SELECT title
  FROM provider_release_entries
  WHERE change_type = 'new_list_resource'
  ORDER BY order_index"
`

## Contributors

We welcome contributions from the community! Whether it's reporting a bug, suggesting a new feature, or submitting a pull request, your input is highly valued.

For more information, please see our contribution [guidelines](./CONTRIBUTING.md). <br><br>

<a href="https://github.com/dkooll/aztfmcp/graphs/contributors">
  <img src="https://contrib.rocks/image?repo=dkool/aztfmcp" />
</a>

## License

MIT Licensed. See [LICENSE](./LICENSE) for full details.
