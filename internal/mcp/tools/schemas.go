package tools

import "encoding/json"

// Input schemas for MCP tools (draft-7 compatible objects).

var inputDiscoverOperations = json.RawMessage(`{
  "type": "object",
  "required": ["schema_path"],
  "properties": {
    "schema_path": {
      "type": "string",
      "description": "Absolute or relative path to an OpenAPI 3.x schema file (YAML or JSON)"
    }
  }
}`)

var inputSuggestStrategy = json.RawMessage(`{
  "type": "object",
  "required": ["operations"],
  "properties": {
    "operations": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["id", "method"],
        "properties": {
          "id":       { "type": "string" },
          "method":   { "type": "string" },
          "has_body": { "type": "boolean" }
        }
      }
    }
  }
}`)

var inputValidate = json.RawMessage(`{
  "type": "object",
  "required": ["config_path"],
  "properties": {
    "config_path": {
      "type": "string",
      "description": "Path to a backendtest.yaml config file"
    }
  }
}`)

var inputScaffoldConfig = json.RawMessage(`{
  "type": "object",
  "required": ["schema_path"],
  "properties": {
    "schema_path": {
      "type": "string",
      "description": "Path to an OpenAPI 3.x schema file"
    },
    "base_url": {
      "type": "string",
      "description": "Target base URL, e.g. http://localhost:8080. Defaults to http://localhost:8080 if omitted."
    },
    "strategies": {
      "type": "array",
      "items": { "type": "string", "enum": ["table", "property", "fuzz", "contract"] },
      "description": "Strategies to include. Defaults to [table] if omitted."
    },
    "output_path": {
      "type": "string",
      "description": "If provided, write the generated config to this path on disk. If omitted, return it as a string only."
    }
  }
}`)

var inputRun = json.RawMessage(`{
  "type": "object",
  "required": ["config_path"],
  "properties": {
    "config_path": {
      "type": "string",
      "description": "Path to a backendtest.yaml config file"
    },
    "strategy": {
      "type": "string",
      "enum": ["table", "property", "fuzz"],
      "description": "Strategy to run. Defaults to the first strategy in the config if omitted."
    },
    "seed": {
      "type": "integer",
      "description": "Seed for deterministic property or fuzz runs. 0 means random."
    }
  }
}`)

var inputExplainFailure = json.RawMessage(`{
  "type": "object",
  "required": ["artifact_path"],
  "properties": {
    "artifact_path": {
      "type": "string",
      "description": "Path to a .json artifact file produced by bt_run or bt run"
    }
  }
}`)
