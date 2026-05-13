import json
import subprocess
import sys


def main():
    json_path = sys.argv[1] if len(sys.argv) > 1 else "/tmp/lmd-rebuild.json"
    lmd_binary = sys.argv[2] if len(sys.argv) > 2 else "lmd"

    try:
        with open(json_path) as f:
            data = json.load(f)
    except (FileNotFoundError, json.JSONDecodeError) as e:
        print(f"Error: cannot read {json_path}: {e}", file=sys.stderr)
        sys.exit(1)

    collections = data.get("collections", [])
    if not collections:
        print("No collections to re-add.")
        return

    for c in collections:
        name = c["name"]
        col_path = c["path"]

        if name.startswith("@"):
            # Skip system collections (@hyde, @summaries, etc.)
            print(f"Skip system collection: {name}", file=sys.stderr)
            continue

        cmd = [lmd_binary, "collection", "add", col_path, "--name", name]
        result = subprocess.run(cmd, capture_output=True, text=True)

        if result.returncode == 0:
            sys.stdout.write(result.stdout)
        else:
            print(f"Warning: re-add {name} failed: {result.stderr.strip()}", file=sys.stderr)


if __name__ == "__main__":
    main()
