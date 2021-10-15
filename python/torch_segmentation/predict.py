import sys
from pathlib import Path

sys.path.insert(0, '..')

import cog
import cv2
import torch
import torchvision
from PIL import Image

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

    # Define the arguments and types the model takes as input
    @cog.input("input", type=Path, help="Image to classify")
    def predict(self, input):
        """Run a single prediction on the model"""
        # download or load the model from disk

        # read the image
        image = Image.open(input)
        # do forward pass and get the output dictionary
        device = torch.device('cuda' if torch.cuda.is_available() else 'cpu')
        try:
            outputs = segmentation_utils.get_segment_labels(image, self.model, device)
        except Exception as e:
            print(e)
        # get the data from the `out` key
        outputs = outputs['out']
        segmented_image = segmentation_utils.draw_segmentation_map(outputs)

        final_image = segmentation_utils.image_overlay(image, segmented_image)
        save_name = f"{input.split('/')[-1].split('.')[0]}"
        # # show the segmented image and save to disk
        # cv2.imshow('Segmented image', final_image)
        # cv2.waitKey(0)
        cv2.imwrite(f"outputs/{save_name}.jpg", final_image)
        print(f"Saving file at: outputs/{save_name}.jpg")
        return Path(f"outputs/{save_name}.jpg")

# Usage: uncomment this and run
# python predict.py
# if __name__ == "__main__":
#     obj = Segmentation()
#     obj.setup()
#     obj.predict(input='street2.jpg')
