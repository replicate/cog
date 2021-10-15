import random
import string
import sys

sys.path.insert(0, '../..')

import cog
import cv2
import torch
import torchvision
from PIL import Image
from pathlib import Path
import platform
import tempfile
import segmentation_utils


# Usage: cog predict -i @street2.jpg

class Segmentation(cog.Predictor):
    def setup(self):
        """Load the model into memory to make running multiple predictions efficient"""
        self.model = torchvision.models.segmentation.fcn_resnet50(pretrained=True)
        # set computation device
        device = torch.device('cuda' if torch.cuda.is_available() else 'cpu')
        # model to eval() model and load onto computation device
        self.model.eval().to(device)

    @cog.input("input", type=Path, help="Image to segment")
    def predict(self, input):

        # read the image
        image = Image.open(input)
        # do forward pass and get the output dictionary
        device = torch.device('cuda' if torch.cuda.is_available() else 'cpu')
        outputs = segmentation_utils.get_segment_labels(image, self.model, device)

        # get the data from the `out` key
        outputs = outputs['out']
        segmented_image = segmentation_utils.draw_segmentation_map(outputs)

        final_image = segmentation_utils.image_overlay(image, segmented_image)
        # # show the segmented image and save to disk
        # cv2.imshow('Segmented image', final_image)
        # cv2.waitKey(0)

        save_name = ''.join(random.choice(string.ascii_uppercase + string.digits) for _ in range(10))
        tempdir = Path("/tmp" if platform.system() == "Darwin" else tempfile.gettempdir())
        cv2.imwrite(f"{tempdir}/{save_name}_out.jpg", final_image)
        return Path(f"{tempdir}/{save_name}_out.jpg")

# Usage: uncomment this and run the below command to run directly
# python predict.py
# if __name__ == "__main__":
#     obj = Segmentation()
#     obj.setup()
#     obj.predict(input='street2.jpg')
