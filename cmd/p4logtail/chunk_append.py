#!/usr/bin/env python3
"""
Script to append file contents in chunks of 1000 lines every second
Usage: python chunk_append.py <input_file> <output_file>

Useful for testing p4logtail with steadily incrementing log files, simulating a live log file being written to.
"""

import sys
import time
import os

def chunk_append(input_file, output_file, chunk_size=1000, delay=1):
    """
    Read input file and append contents to output file in chunks
    
    Args:
        input_file (str): Path to input file
        output_file (str): Path to output file
        chunk_size (int): Number of lines per chunk (default: 1000)
        delay (int): Delay in seconds between chunks (default: 1)
    """
    
    # Check if input file exists
    if not os.path.exists(input_file):
        print(f"Error: Input file '{input_file}' does not exist.")
        return False
    
    try:
        with open(input_file, 'r', encoding='utf-8') as infile:
            chunk_count = 0
            
            while True:
                # Read chunk_size lines
                lines = []
                for _ in range(chunk_size):
                    line = infile.readline()
                    if not line:  # End of file
                        break
                    lines.append(line)
                
                # If no lines were read, we've reached the end
                if not lines:
                    break
                
                # Append chunk to output file
                with open(output_file, 'a', encoding='utf-8') as outfile:
                    outfile.writelines(lines)
                
                chunk_count += 1
                print(f"Appended chunk {chunk_count} ({len(lines)} lines) to {output_file}")
                
                # If we read fewer lines than chunk_size, we've reached the end
                if len(lines) < chunk_size:
                    break
                
                # Wait before processing next chunk
                time.sleep(delay)
        
        print(f"Complete! Processed {chunk_count} chunks from '{input_file}' to '{output_file}'")
        return True
        
    except IOError as e:
        print(f"Error reading/writing files: {e}")
        return False
    except KeyboardInterrupt:
        print(f"\nOperation interrupted by user. Processed {chunk_count} chunks.")
        return False

def main():
    if len(sys.argv) != 3:
        print("Usage: python chunk_append.py <input_file> <output_file>")
        print("Example: python chunk_append.py data.txt output.txt")
        sys.exit(1)
    
    input_file = sys.argv[1]
    output_file = sys.argv[2]
    
    print(f"Starting to append '{input_file}' to '{output_file}' in 1000-line chunks...")
    chunk_append(input_file, output_file)

if __name__ == "__main__":
    main()
