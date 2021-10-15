## Using pytorch fcn_resnet50 pretrained inside cog

This code uses cog to put a pretrained `fcn_resnet50` behind an API (flask server) that processes POST image and returns
a segmented image.

FCN-ResNet is constructed by a Fully-Convolutional Network model, using a ResNet-50 or a ResNet-101 backbone. The
pre-trained models have been trained on a subset of COCO train2017, on the 20 categories that are present in the Pascal
VOC dataset. You can read more about it here: https://pytorch.org/hub/pytorch_vision_fcn_resnet101/

This code can be easily adapted for any other pre-trained segmentation models as well in pytorch.

## Build

```
    cog build -t torch_segmentation_example
```

## Predict

```
    cog predict -i @street2.jpg
```

## Deploy/Run

```
    docker run -d -p 5000:5000 segment
```

## Call endpoinnt

You can then send a POST request using below code or using a tool like POSTMAN

```
    curl http://localhost:5000/predict -X POST -F input=@street2.jpg
```
