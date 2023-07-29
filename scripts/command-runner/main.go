package main

import (
	"command-runner/tools"
	"flag"
	"fmt"
	"os"
)

// Define the output JSON file path and name
// const outputJSONFilePath = "/home/perforce/workspace/command-runner/output.json" #TODO I see a potential issue if this differs from the main script running this
var (
	outputJSONFilePath   string
	yamlCommandsFilePath string
)

func init() {
	flag.StringVar(&outputJSONFilePath, "output", "output.json", "Path to the output JSON file")
	flag.StringVar(&yamlCommandsFilePath, "comyaml", "commands.yaml", "Path to the YAML file containing shell commands")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [OPTIONS]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
	}
}

func main() {
	// Define a flag for the instance argument
	instanceArg := flag.String("instance", "", "Instance argument for the command-runner")

	// Define a flag for the server argument
	serverArg := flag.Bool("server", false, "Server argument for the command-runner")

	flag.Parse()

	// Check if the server argument is provided
	if *serverArg {
		// If server argument is provided, remove the existing output.json file (if it exists)
		err := os.Remove(outputJSONFilePath)
		if err != nil && !os.IsNotExist(err) {
			fmt.Println("Error removing output.json:", err)
			os.Exit(1)
		}

		// Execute and encode server commands
		serverCommands, err := tools.ReadServerCommandsFromYAML(yamlCommandsFilePath)
		if err != nil {
			fmt.Println("Error reading server commands from YAML:", err)
			os.Exit(1)
		}

		base64ServerOutputs, err := tools.ExecuteAndEncodeCommands(serverCommands)
		if err != nil {
			fmt.Println("Error executing server commands:", err)
			os.Exit(1)
		}

		// Create JSON data for server commands and write to output.json
		var serverJSONData []tools.JSONData
		for i, cmd := range serverCommands {
			serverJSONData = append(serverJSONData, tools.JSONData{
				Command:     cmd.Description,
				Description: cmd.Description,
				Output:      base64ServerOutputs[i],
				MonitorTag:  cmd.MonitorTag,
			})
		}

		if err := tools.WriteJSONToFile(serverJSONData, outputJSONFilePath); err != nil {
			fmt.Printf("Error writing server JSON data to %s: %s\n", outputJSONFilePath, err)
			os.Exit(1)
		}

		// Get the hostname of the server
		hostname, err := os.Hostname()
		if err != nil {
			fmt.Println("Error getting hostname:", err)
			os.Exit(1)
		}

		fmt.Printf("%s Server commands executed and output written to %s.\n", hostname, outputJSONFilePath)

	}

	// Check if the instance argument is provided
	if *instanceArg != "" {
		// Execute and encode instance commands
		instanceCommands, err := tools.ReadInstanceCommandsFromYAML(yamlCommandsFilePath)
		if err != nil {
			fmt.Println("Error reading instance commands from YAML:", err)
			os.Exit(1)
		}

		base64InstanceOutputs, err := tools.ExecuteAndEncodeCommands(instanceCommands)
		if err != nil {
			fmt.Println("Error executing instance commands:", err)
			os.Exit(1)
		}

		// Create JSON data for instance commands
		var instanceJSONData []tools.JSONData
		for i, cmd := range instanceCommands {
			instanceJSONData = append(instanceJSONData, tools.JSONData{
				Command:     cmd.Description,
				Description: cmd.Description,
				Output:      base64InstanceOutputs[i],
				MonitorTag:  cmd.MonitorTag,
			})
		}

		// Append instance JSON data to output.json
		existingJSONData, err := tools.ReadJSONFromFile(outputJSONFilePath)
		if err != nil {
			fmt.Printf("Error reading existing JSON data from %s: %s\n", outputJSONFilePath, err)
			os.Exit(1)
		}

		allJSONData := append(existingJSONData, instanceJSONData...)

		if err := tools.WriteJSONToFile(allJSONData, outputJSONFilePath); err != nil {
			fmt.Printf("Error writing instance JSON data to %s: %s\n", outputJSONFilePath, err)
			os.Exit(1)
		}

		fmt.Printf("Instance %s commands executed and output appended to %s.\n", *instanceArg, outputJSONFilePath)
	}

	if flag.NFlag() == 0 {
		flag.Usage()
	}
}
