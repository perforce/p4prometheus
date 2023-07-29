import json
import sys


def parse_instance_data(file_path):
    with open(file_path, 'r') as file:
        lines = file.readlines()

    instances = []
    current_instance = None

    for line in lines:
        if line.startswith("TIGGER: "):
            continue  # Skip lines with "random serialZ:" and "TIGGER:"
        if line.startswith("#T1GL "):
            if current_instance:
                instances.append(current_instance)
            current_instance = {"Label": line.strip().split("#T1GL ")[1]}
        elif line.strip() != '':
            if "Data" not in current_instance:
                current_instance["Data"] = []
            current_instance["Data"].append(line.strip())

    # Append the last instance
    if current_instance:
        instances.append(current_instance)

    return instances

if __name__ == "__main__":
    if len(sys.argv) != 3:
        print("Usage: python script_name.py <input_file> <output_file>")
        sys.exit(1)

    input_file_path = sys.argv[1]
    output_file_path = sys.argv[2]

    parsed_data = parse_instance_data(input_file_path)

    with open(output_file_path, 'w') as output_file:
        json.dump(parsed_data, output_file, indent=2)

    print("Data parsed and written to", output_file_path)
