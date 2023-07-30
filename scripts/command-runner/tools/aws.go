package tools

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"time"
)

// GetAWSToken retrieves the AWS metadata token.
func GetAWSToken() (string, error) {
	autoCloudTimeout := 5 * time.Second

	tokenURL := "http://169.254.169.254/latest/api/token"
	req, err := http.NewRequest("PUT", tokenURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-aws-ec2-metadata-token-ttl-seconds", "21600")
	client := &http.Client{Timeout: autoCloudTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	token, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(token), nil
}

// GetAWSInstanceIdentityInfo retrieves the instance identity document and tags from the AWS metadata service.
func GetAWSInstanceIdentityInfo(outputFilePath string) error {
	token, err := GetAWSToken()
	if err != nil {
		return err
	}

	documentURL := "http://169.254.169.254/latest/dynamic/instance-identity/document"
	documentOUT, err := getAWSEndpoint(token, documentURL)
	if err != nil {
		return err
	}
	fmt.Println("Instance Identity Document Raw:")
	fmt.Println(string(documentOUT)) // Debug print to see the raw documentOUT content

	metadataURL := "http://169.254.169.254/latest/meta-data/tags/instance/"
	metadataOUT, err := getAWSEndpoint(token, metadataURL)
	if err != nil {
		return err
	}
	fmt.Println("Metadata Raw:")
	fmt.Println(string(metadataOUT)) // Debug print to see the raw metadataOUT content

	// Get the existing JSON data from the file
	existingJSONData, err := ReadJSONFromFile(outputFilePath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	// Append the Base64 encoded documentOUT and metadataOUT to the JSON data

	existingJSONData = append(existingJSONData, JSONData{
		Command:     "Instance Identity Document",
		Description: "AWS Instance Identity Document",
		Output:      EncodeToBase64(string(documentOUT)),
		MonitorTag:  "AWS",
	})

	metadataJSON := JSONData{
		Command:     "Metadata",
		Description: "AWS Metadata",
		Output:      EncodeToBase64(string(metadataOUT)),
		MonitorTag:  "AWS",
	}

	existingJSONData = append(existingJSONData, metadataJSON)

	// Write the updated JSON data back to the file
	if err := WriteJSONToFile(existingJSONData, outputFilePath); err != nil {
		return err
	}

	return nil
}

func getAWSEndpoint(token, url string) ([]byte, error) {
	// Clean the URL to remove unwanted characters
	url = strings.TrimSpace(url)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-aws-ec2-metadata-token", token)
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == http.StatusNotFound {
		// If the response is 404, return the content as is without treating it as an error
		return body, nil
	} else if resp.StatusCode != http.StatusOK {
		// If the response status code is not 200 OK or 404 Not Found, return an error
		return nil, fmt.Errorf("unexpected response status: %s", resp.Status)
	}

	return body, nil
}
