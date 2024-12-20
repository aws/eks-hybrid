## Calico template

The calico-template.yaml is updated by running:
```sh
./update-manifests.sh v3.29.0
```

*Optional*: The images are automatically pulled via an ECR pull through cache. Testing your version changes, pulling these images from the team build
account will precache the images for CI in us-west-2.  If you want to manually precache these in both us-west-1 and us-west-2, which replicate
to the other commerical regions, run:
```sh
./mirror-calico.sh v3.29.0
```
