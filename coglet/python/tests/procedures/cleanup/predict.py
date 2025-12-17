import os
import tempfile


def predict(test_id: int) -> str:
    uid = os.getuid()
    gid = os.getgid()

    print(f'Test {test_id}: UID={uid}, GID={gid}')

    # Create test files in /tmp
    files_created = []

    # Create regular files
    for i in range(3):
        filepath = f'/tmp/cleanup-{uid}-{test_id}-{i}.txt'
        with open(filepath, 'w') as f:
            f.write(f'Test {test_id}, UID {uid}, file {i}')
        files_created.append(filepath)
        print(f'Created: {filepath}')

    # Create directory with nested files
    dirpath = f'/tmp/cleanup-{uid}-{test_id}-dir'
    os.makedirs(dirpath, exist_ok=True)
    nested_file = f'{dirpath}/nested.txt'
    with open(nested_file, 'w') as f:
        f.write(f'Nested file for test {test_id}, UID {uid}')
    files_created.append(dirpath)
    print(f'Created directory: {dirpath}')

    # Verify files exist
    for path in files_created:
        if os.path.exists(path):
            print(f'Verified exists: {path}')

    # Also create a file in TMPDIR (should be cleaned by normal cleanup)
    with tempfile.NamedTemporaryFile(
        mode='w', delete=False, prefix='tmpdir-test-'
    ) as f:
        f.write(f'TMPDIR file for test {test_id}')
        print(f'Created in TMPDIR: {f.name}')

    return f'Test {test_id} created {len(files_created)} items in /tmp for UID {uid}'
