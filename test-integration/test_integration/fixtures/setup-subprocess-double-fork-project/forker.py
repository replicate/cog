import os
import signal
import time


def main():
    child_pid = os.fork()
    is_child = child_pid == 0

    pid = os.getpid()
    was_pinged = False

    while True:
        if os.path.exists(".inbox") and is_child:
            s = ""

            with open(".inbox", "r") as inbox:
                print(f"---> CHILD ({pid}) reading request")

                s = inbox.read()

            os.unlink(".inbox")

            with open(".outbox", "w") as outbox:
                print(f"---> CHILD ({pid}) sending response")

                outbox.write("hello " + s)

        if time.time() % 10 == 0:
            if is_child:
                print(f"---> CHILD ({pid}) " + ("here " * 20))
            else:
                print(f"===> PARENT ({pid})")

        time.sleep(0.01)


if __name__ == "__main__":
    main()
