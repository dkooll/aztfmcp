# azurerm-mcp

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
    "az-cn-azurerm": {
      "command": "/path/to/az-cn-azurerm-mcp",
      "args": ["--org", "hashicorp", "--repo", "terraform-provider-azurerm", "--token", "YOUR_TOKEN"]
    }
  }
}
```

## Build from source

make build

## Example Prompts

**Once configured, you can ask any agentic agent that supports additional MCP servers:**

**Update Behavior & Breaking Changes**

Can I change the location on azurerm_resource_group without recreating it, and if not, why?

Does updating address_space on azurerm_virtual_network require recreation or can it be done in-place?

Why is dns_prefix marked as ForceNew on azurerm_kubernetes_cluster and what's my migration path?

Explain why changing the SKU on azurerm_storage_account forces recreation and suggest workarounds.

Will modifying network_profile on azurerm_container_group trigger a recreation?

**Resource Comparison & Discovery**

What are the differences between azurerm_linux_virtual_machine and azurerm_windows_virtual_machine?

Compare azurerm_network_security_group and azurerm_network_security_rule schemas side-by-side.

Show me resources similar to azurerm_kubernetes_cluster with at least 70% schema similarity.

Find all resources that have similar schemas to azurerm_storage_account, ranked by similarity.

What's the schema overlap between azurerm_app_service and azurerm_function_app?

**Schema Deep Dive**

Show me all ForceNew attributes on azurerm_virtual_network and explain each one.

What attributes on azurerm_storage_account are marked sensitive?

List all nested blocks in azurerm_kubernetes_cluster and their cardinality.

Show the full schema for azurerm_private_endpoint with type details and validations.

What conflicts exist on azurerm_api_management_certificate attributes?

**Validation Analysis**

What validations are missing from azurerm_storage_account schema?

Analyze azurerm_virtual_network for weak or missing field validations.

Does azurerm_container_registry have proper port range validations?

Check if azurerm_key_vault has appropriate name format validations.

Find validation gaps in azurerm_postgresql_server configuration.

**Dependency Tracing**

Show me all dependencies for the network_rules attribute on azurerm_storage_account.

What attributes conflict with connection_string on azurerm_eventhub?

Trace the ExactlyOneOf constraints for azurerm_data_factory_linked_service_sftp authentication.

What attributes require subnet_id on azurerm_app_service?

Visualize the dependency graph for identity blocks in azurerm_virtual_machine_scale_set.

**Provider Source Inspection**

Show me the CustomizeDiff logic for azurerm_virtual_network.

What timeout configurations exist for azurerm_kubernetes_cluster?

Show the importer function for azurerm_storage_account.

Display the full Go schema definition for azurerm_private_endpoint.

What state upgraders are defined for azurerm_app_service?

**Search & Discovery**

Search the provider code for suppress.CaseDifference usage.

Find all resources that use validation.IntBetween for port validation.

Show me resources with more than 8 ForceNew attributes.

List data sources that query network-related Azure resources.

Find resources in the compute service with CustomizeDiff logic.

**Releases & Versioning**

What’s the latest AzureRM provider release? Summarize features, enhancements, and bug fixes.

What shipped in v4.48.0? Include the tag range and release date.

Show the code diff snippet for azurerm_windows_web_app adding virtual_network_image_pull_enabled in v4.52.0.

Show the code change for the new resource azurerm_api_management_workspace_api_version_set in v4.52.0.

Compare v4.51.0 → v4.52.0 and list any new resources and notable fixes.

If a version isn’t indexed yet, backfill v4.48.0 and then summarize it.

**Testing & Documentation**

List all acceptance tests for azurerm_kubernetes_cluster.

Show the Example Usage section from azurerm_virtual_network documentation.

What tests cover azurerm_storage_account blob features?

Get the full documentation for azurerm_private_endpoint including arguments.

Show me provider examples for virtual machine deployments.

**Cross-Reference with WAM MCP**

Are there any missing key things or features if you compare the wam module agw mcp with the terraform providider mcp resource azurerm_application_gateway regarding the module examples?

What does the CloudNation vnet module expose that azurerm_virtual_network doesn't enforce through validation?

Compare the kv module examples from WAM with azurerm_key_vault schema ,are there any attributes the module uses that have weak validations in the provider?

Looking at the redis module in WAM, does it use any azurerm_redis_cache attributes that are marked ForceNew, and would those prevent in-place updates?

What required attributes does azurerm_storage_account have that the CloudNation storage module doesn't expose as variables?

Does the WAM pe module handle all the ConflictsWith constraints that exist in azurerm_private_endpoint?

Check if the CloudNation aks module examples use any azurerm_kubernetes_cluster attributes that would trigger recreation on change.

What validation patterns does the agw module implement that should also exist in the azurerm_application_gateway schema?

Are there sensitive attributes in azurerm_container_registry that the acr module exposes without marking them sensitive?

Compare the default values in the CloudNation func module with the azurerm_function_app schema defaults.

**Sync and Maintenance**

Run a full sync of the provider and report the job ID; then show the sync status for that job ID.

Run an incremental sync (updates only) and report the job ID; then show the sync status for that job ID.

What's the current provider sync status?

**Tips**
```
For schema inspection, use compact: true to get bullet lists instead of full tables.

Use flags like ["force_new"] or ["required", "sensitive"] to filter attributes.

For similar resource discovery, adjust similarity_threshold (0.0-1.0) to be more or less strict.

Resource comparisons work best when comparing resources in the same service area.

Breaking change explanations include both technical reasons and Azure API limitations.
```

## Notes

GitHub token is optional; without it, syncing still works but may hit lower API rate limits. Pass `--token` to raise limits.

Initial full sync takes ~20 seconds and indexes 9,000+ Go files. Subsequent incremental syncs are much faster.

Deleting the database file will cause a full rebuild the next time the server is called.

The parser extracts schema metadata using Go AST analysis for accuracy.

Release summaries maintain the most recent 40 versions by default; older tags can be backfilled on demand when needed.

## Direct Database Access

The indexed data is stored in a SQLite database file with FTS5 enabled. You can query it directly for ad‑hoc inspection:

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

## Contributors

We welcome contributions from the community! Whether it's reporting a bug, suggesting a new feature, or submitting a pull request, your input is highly valued.

For more information, please see our contribution [guidelines](./CONTRIBUTING.md). <br><br>

<a href="https://github.com/dkooll/aztfmcp/graphs/contributors">
  <img src="https://contrib.rocks/image?repo=dkool/aztfmcp" />
</a>

## License

MIT Licensed. See [LICENSE](./LICENSE) for full details.
