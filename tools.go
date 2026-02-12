package main

import (
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/shared"
)

// ToolDefinitions returns the 5 tool schemas the LLM can call.
func ToolDefinitions() []openai.ChatCompletionToolParam {
	return []openai.ChatCompletionToolParam{
		grepTool(),
		findFilesTool(),
		readFileTool(),
		listDirTool(),
		submitAnswerTool(),
	}
}

func grepTool() openai.ChatCompletionToolParam {
	return openai.ChatCompletionToolParam{
		Function: shared.FunctionDefinitionParam{
			Name:        "grep",
			Description: openai.String("Search file contents using ripgrep. Returns matching lines with file paths and line numbers."),
			Parameters: shared.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"pattern": map[string]any{
						"type":        "string",
						"description": "Regular expression pattern to search for.",
					},
					"glob": map[string]any{
						"type":        "string",
						"description": "File glob to restrict search (e.g. \"*.go\", \"src/**/*.ts\").",
					},
					"max_results": map[string]any{
						"type":        "integer",
						"description": "Maximum number of matches to return (default 30, max 100).",
					},
				},
				"required": []string{"pattern"},
			},
		},
	}
}

func findFilesTool() openai.ChatCompletionToolParam {
	return openai.ChatCompletionToolParam{
		Function: shared.FunctionDefinitionParam{
			Name:        "find_files",
			Description: openai.String("Find files and directories by glob pattern. Returns paths relative to project root."),
			Parameters: shared.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"pattern": map[string]any{
						"type":        "string",
						"description": "Glob pattern to match (supports ** for recursive matching).",
					},
					"type": map[string]any{
						"type":        "string",
						"enum":        []string{"file", "dir"},
						"description": "Filter by entry type: 'file' or 'dir'.",
					},
				},
				"required": []string{"pattern"},
			},
		},
	}
}

func readFileTool() openai.ChatCompletionToolParam {
	return openai.ChatCompletionToolParam{
		Function: shared.FunctionDefinitionParam{
			Name:        "read_file",
			Description: openai.String("Read the contents of a file with line numbers. Paths are relative to the project root."),
			Parameters: shared.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "File path relative to project root.",
					},
					"start_line": map[string]any{
						"type":        "integer",
						"description": "First line to read (1-based, inclusive).",
					},
					"end_line": map[string]any{
						"type":        "integer",
						"description": "Last line to read (1-based, inclusive).",
					},
				},
				"required": []string{"path"},
			},
		},
	}
}

func listDirTool() openai.ChatCompletionToolParam {
	return openai.ChatCompletionToolParam{
		Function: shared.FunctionDefinitionParam{
			Name:        "list_dir",
			Description: openai.String("List directory contents as an indented tree. Defaults to project root at depth 2."),
			Parameters: shared.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "Directory path relative to project root (default: \".\").",
					},
					"depth": map[string]any{
						"type":        "integer",
						"description": "Maximum depth to recurse (default 2).",
					},
				},
			},
		},
	}
}

func submitAnswerTool() openai.ChatCompletionToolParam {
	return openai.ChatCompletionToolParam{
		Function: shared.FunctionDefinitionParam{
			Name:        "submit_answer",
			Description: openai.String("Submit the final answer with file locations and a summary. Call this when you have found the relevant code."),
			Parameters: shared.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"results": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"file": map[string]any{
									"type":        "string",
									"description": "File path relative to project root.",
								},
								"start_line": map[string]any{
									"type":        "integer",
									"description": "Start line of relevant section.",
								},
								"end_line": map[string]any{
									"type":        "integer",
									"description": "End line of relevant section.",
								},
								"reason": map[string]any{
									"type":        "string",
									"description": "Why this location is relevant.",
								},
							},
							"required": []string{"file", "start_line", "end_line", "reason"},
						},
						"description": "Array of file locations with line ranges and reasons.",
					},
					"summary": map[string]any{
						"type":        "string",
						"description": "Brief summary of findings.",
					},
				},
				"required": []string{"results", "summary"},
			},
		},
	}
}
