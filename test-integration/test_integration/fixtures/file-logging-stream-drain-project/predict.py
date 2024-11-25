from cog import BasePredictor, File
import logging
import os
from PIL import Image


class Predictor(BasePredictor):
    def setup(self):
        logFormatter = logging.Formatter("%(asctime)s [%(threadName)-12.12s] [%(levelname)-5.5s]  %(message)s")
        rootLogger = logging.getLogger()

        fileHandler = logging.FileHandler("{0}/{1}.log".format(os.path.dirname(__file__), "mylog.log"))
        fileHandler.setFormatter(logFormatter)
        rootLogger.addHandler(fileHandler)

        consoleHandler = logging.StreamHandler()
        consoleHandler.setFormatter(logFormatter)
        rootLogger.addHandler(consoleHandler)

    def predict(self) -> list[Image]:
        logging.info("Do some logging.")
        return []
