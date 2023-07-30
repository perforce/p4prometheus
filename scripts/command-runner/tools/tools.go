package tools

import (
	"encoding/base64"
	"encoding/json"
	"io/ioutil"
	"os"
	"os/exec"

	"gopkg.in/yaml.v2"
)

// Define a struct to hold each command's details
type Command struct {
	Description string `yaml:"description"`
	Command     string `yaml:"command"`
	MonitorTag  string `yaml:"monitor_tag"`
}

// Define a struct to hold the configuration from the YAML file for instance_commands and server_commands
//#TODO Is this needed
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
	commands := make([]Command, 0)

	instanceCommands, err := ReadInstanceCommandsFromYAML(filePath)
	if err != nil {
		return nil, err
	}
	commands = append(commands, instanceCommands...)

	serverCommands, err := ReadServerCommandsFromYAML(filePath)
	if err != nil {
		return nil, err
	}
	commands = append(commands, serverCommands...)

	return commands, nil
}

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

func EncodeToBase64(input string) string {
	return base64.StdEncoding.EncodeToString([]byte(input))
}

// Function to write JSON data to a file with indentation for human-readability.....Minus the base64 humans don't read that stuff... normally
func WriteJSONToFile(data []JSONData, filePath string) error {
	jsonString, err := json.MarshalIndent(data, "", "    ") // Use four spaces for indentation
	if err != nil {
		return err
	}

	// Check if the file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		// Create the file if it doesn't exist
		file, err := os.Create(filePath)
		if err != nil {
			return err
		}
		defer file.Close()
	}

	return ioutil.WriteFile(filePath, jsonString, 0644)
}

func ReadJSONFromFile(filePath string) ([]JSONData, error) {
	jsonFile, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer jsonFile.Close()

	var jsonData []JSONData
	dec := json.NewDecoder(jsonFile)
	if err := dec.Decode(&jsonData); err != nil {
		return nil, err
	}

	return jsonData, nil
}
