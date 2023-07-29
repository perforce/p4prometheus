import argparse
import json
import os

def parse_to_markdown(data_list):
    parsed_data = {}
    for item in data_list:
        label = item["Label"]
        data = item["Data"]
        p4tigtag = None

        for line in data:
            if line.startswith("P4TIGTAG:"):
                p4tigtag = line.split(":")[1].strip()
                break

        if p4tigtag:
            if p4tigtag not in parsed_data:
                parsed_data[p4tigtag] = []
            parsed_data[p4tigtag].append({"Label": label, "Data": data})

    return parsed_data
def write_to_files(parsed_data, output_dir):
    for p4tigtag, data_list in parsed_data.items():
        filename = os.path.join(output_dir, f"{p4tigtag.lower()}_output.md")
        with open(filename, "w") as f:
            for item in data_list:
                f.write(f"# {item['Label']}\n\n")
                for line in item['Data']:
                    f.write(f"{line}\n")
                f.write("\n")

if __name__ == "__main__":
    # Argument parsing
    parser = argparse.ArgumentParser(description="Convert JSON data to Markdown files.")
    parser.add_argument("input_file", type=str, help="Path to the input JSON file.")
    parser.add_argument("output_dir", type=str, help="Directory where the output Markdown files will be saved.")
    args = parser.parse_args()

    # Read input JSON data
    with open(args.input_file, "r") as json_file:
        json_data = json.load(json_file)

    # Parse data and generate Markdown files
    parsed_data = parse_to_markdown(json_data)
    write_to_files(parsed_data, args.output_dir)
