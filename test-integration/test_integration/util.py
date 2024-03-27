import random
import string
import subprocess
import time


def random_string(length):
    return "".join(random.choice(string.ascii_lowercase) for i in range(length))


def remove_docker_image(image_name, max_attempts=5, wait_seconds=1):
    for attempt in range(max_attempts):
        try:
            subprocess.run(
                ["docker", "rmi", "-f", image_name], check=True, capture_output=True
            )
            print(f"Image {image_name} successfully removed.")
            break
        except subprocess.CalledProcessError as e:
            print(f"Attempt {attempt + 1} failed: {e.stderr.decode()}")
            time.sleep(wait_seconds)
    else:
        print(f"Failed to remove image {image_name} after {max_attempts} attempts.")
