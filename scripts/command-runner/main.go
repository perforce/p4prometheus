package main

import (
	"command-runner/tools"
	"flag"
	"fmt"
	"os"
)

var (
	outputJSONFilePath   string
	yamlCommandsFilePath string
	cloudProvider        string
)

func init() {
	flag.StringVar(&outputJSONFilePath, "output", "out.json", "Path to the output JSON file")
	flag.StringVar(&yamlCommandsFilePath, "comyaml", "commands.yaml", "Path to the YAML file containing shell commands")
	flag.StringVar(&cloudProvider, "cloud", "", "Cloud provider (aws, gcp, or azure)")
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

	// If -cloud is provided, check if it's a valid cloud provider
	if cloudProvider != "" {
		switch cloudProvider {
		case "aws":
			// Logic to handle AWS-related functionality
			err := tools.GetAWSInstanceIdentityInfo(outputJSONFilePath)
			if err != nil {
				fmt.Println("Error getting AWS instance identity info:", err)
				os.Exit(1)
			}
		case "gcp":
			// Add logic to handle GCP-related functionality
		case "azure":
			// Add logic to handle Azure-related functionality
		default:
			fmt.Println("Error: Invalid cloud provider. Please specify aws, gcp, or azure.")
			os.Exit(1)
		}
	}

	// Check if the server argument is provided
	if *serverArg {
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

		// Create JSON data for server commands
		var serverJSONData []tools.JSONData
		for i, cmd := range serverCommands {
			serverJSONData = append(serverJSONData, tools.JSONData{
				Command:     cmd.Description,
				Description: cmd.Description,
				Output:      base64ServerOutputs[i],
				MonitorTag:  cmd.MonitorTag,
			})
		}

		// Get the existing JSON data from the file (if it exists)
		existingJSONData, err := tools.ReadJSONFromFile(outputJSONFilePath)
		if err != nil && !os.IsNotExist(err) {
			fmt.Printf("Error reading existing JSON data from %s: %s\n", outputJSONFilePath, err)
			os.Exit(1)
		}

		// Append server JSON data to existing data
		allJSONData := append(existingJSONData, serverJSONData...)

		// Write the updated JSON data back to the file
		if err := tools.WriteJSONToFile(allJSONData, outputJSONFilePath); err != nil {
			fmt.Printf("Error writing server JSON data to %s: %s\n", outputJSONFilePath, err)
			os.Exit(1)
		}

		// Get the hostname of the server
		hostname, err := os.Hostname()
		if err != nil {
			fmt.Println("Error getting hostname:", err)
			os.Exit(1)
		}

		fmt.Printf("%s Server commands executed and output appended to %s.\n", hostname, outputJSONFilePath)
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
			// Prepend the instance name to the description
			desc := fmt.Sprintf("p4d instance: [%s] %s", *instanceArg, cmd.Description)
			instanceJSONData = append(instanceJSONData, tools.JSONData{
				Command:     cmd.Description,
				Description: desc,
				Output:      base64InstanceOutputs[i],
				MonitorTag:  cmd.MonitorTag,
			})
		}

		// Get the existing JSON data from the file (if it exists)
		existingJSONData, err := tools.ReadJSONFromFile(outputJSONFilePath)
		if err != nil && !os.IsNotExist(err) {
			fmt.Printf("Error reading existing JSON data from %s: %s\n", outputJSONFilePath, err)
			os.Exit(1)
		}

		// Append instance JSON data to existing data
		allJSONData := append(existingJSONData, instanceJSONData...)

		// Write the updated JSON data back to the file
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
