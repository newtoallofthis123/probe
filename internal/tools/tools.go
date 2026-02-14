package tools

import (
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/shared"
)

// SelectModeDefinition returns the select_mode tool for auto mode's first turn.
func SelectModeDefinition() []openai.ChatCompletionToolParam {
	return []openai.ChatCompletionToolParam{selectModeTool()}
}

func selectModeTool() openai.ChatCompletionToolParam {
	return openai.ChatCompletionToolParam{
		Function: shared.FunctionDefinitionParam{
			Name:        "select_mode",
			Description: openai.String("Choose the search mode for this query. Call this first before using any other tools."),
			Parameters: shared.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"mode": map[string]any{
						"type":        "string",
						"enum":        []string{"locate", "explore", "trace"},
						"description": "locate: find where something is. explore: understand how something works across files. trace: follow a call/data path through the code.",
					},
				},
				"required": []string{"mode"},
			},
		},
	}
}

// ToolDefinitions returns the 5 tool schemas the LLM can call.
func ToolDefinitions() []openai.ChatCompletionToolParam {
	return []openai.ChatCompletionToolParam{
		grepTool(),
		findFilesTool(),
		readFileTool(),
		treeTool(),
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

func treeTool() openai.ChatCompletionToolParam {
	return openai.ChatCompletionToolParam{
		Function: shared.FunctionDefinitionParam{
			Name:        "tree",
			Description: openai.String("List directory contents as a tree. Returns indented output with files and subdirectories."),
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
