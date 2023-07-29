package tools

import (
	"encoding/base64"
	"encoding/json"
	"io/ioutil"
	"os/exec"

	"gopkg.in/yaml.v2"
)

// Define a struct to hold each command's details
// Define a struct to hold each command's details
type Command struct {
	Description string `yaml:"description"`
	Command     string `yaml:"command"`
	MonitorTag  string `yaml:"monitor_tag"`
}

// Define a struct to hold the configuration from the YAML file for instance_commands and server_commands
type CommandConfig struct {
	InstanceCommands []Command `yaml:"instance_commands"`
	ServerCommands   []Command `yaml:"server_commands"`
}

type JSONData struct {
	Command     string `json:"command"`
	Description string `json:"description"`
	Output      string `json:"output"`
	MonitorTag  string `json:"monitor_tag"`
}

// Function to read instance_commands from the YAML file
func ReadInstanceCommandsFromYAML(filePath string) ([]Command, error) {
	yamlFile, err := ioutil.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var config CommandConfig
	if err := yaml.Unmarshal(yamlFile, &config); err != nil {
		return nil, err
	}

	return config.InstanceCommands, nil
}

// Function to read server_commands from the YAML file
func ReadServerCommandsFromYAML(filePath string) ([]Command, error) {
	yamlFile, err := ioutil.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var config CommandConfig
	if err := yaml.Unmarshal(yamlFile, &config); err != nil {
		return nil, err
	}

	return config.ServerCommands, nil
}
func ReadCommandsFromYAML(filePath string) ([]Command, error) {
	yamlFile, err := ioutil.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var config CommandConfig
	if err := yaml.Unmarshal(yamlFile, &config); err != nil {
		return nil, err
	}

	var commands []Command
	// Combine both instance and server commands
	commands = append(commands, config.InstanceCommands...)
	commands = append(commands, config.ServerCommands...)

	return commands, nil
}

// Function to execute a single shell command and capture its output
func ExecuteShellCommand(command string) (string, error) {
	cmd := exec.Command("sh", "-c", command)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	return string(output), nil
}

// Function to execute commands and encode output to Base64
func ExecuteAndEncodeCommands(commands []Command) ([]string, error) {
	var base64Outputs []string

	for _, cmd := range commands {
		output, err := ExecuteShellCommand(cmd.Command)
		if err != nil {
			return nil, err
		}
		base64Output := EncodeToBase64(output)
		base64Outputs = append(base64Outputs, base64Output)
	}

	return base64Outputs, nil
}

// Function to encode input to Base64
func EncodeToBase64(input string) string {
	return base64.StdEncoding.EncodeToString([]byte(input))
}

// Function to write JSON data to a file
func WriteJSONToFile(data []JSONData, filePath string) error {
	jsonString, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}

	return ioutil.WriteFile(filePath, jsonString, 0644)
}

// Function to convert the input type to []tools.CommandConfig
func ConvertToCommandConfig(commands []struct {
	Description string `yaml:"description"`
	Command     string `yaml:"command"`
	MonitorTag  string `yaml:"monitor_tag"`
}) CommandConfig {
	var cmdConfig CommandConfig
	for _, cmd := range commands {
		cmdConfig.InstanceCommands = append(cmdConfig.InstanceCommands, Command{
			Description: cmd.Description,
			Command:     cmd.Command,
			MonitorTag:  cmd.MonitorTag,
		})
	}
	return cmdConfig
}
func ReadJSONFromFile(filePath string) ([]JSONData, error) {
	jsonFile, err := ioutil.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var jsonData []JSONData
	if err := json.Unmarshal(jsonFile, &jsonData); err != nil {
		return nil, err
	}

	return jsonData, nil
}
