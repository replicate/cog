import multiprocessing.connection
import multiprocessing.synchronize
import os
import time


def ponger(
    conn: multiprocessing.connection.Connection, lock: multiprocessing.synchronize.Lock
):
    for i in range(100):
        print(f"Getting ready for some serious ponginggg ({i+1}%)")
        time.sleep(0.001 + (0.001 * (i + 1)))

    print("ITS PONGIN TIME")

    pid = os.getpid()

    while True:
        try:
            ping = conn.recv()
            print(f"received {ping} in {pid}")

            with lock:
                print(f"ponging from {pid}")

                conn.send("pong")
                conn.close()

        except EOFError:
            pass
