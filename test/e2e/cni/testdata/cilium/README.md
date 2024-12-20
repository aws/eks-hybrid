## Cilium template

The cilium-template.yaml is generated with helm and the values.yaml is updated by running:
```sh
./update-manifests.sh v1.16.3
```
You can update the cilium version or any other param by re-running this command with the desired version and committing the changes.

*Optional*: The images are automatically pulled via an ECR pull through cache. Testing your version changes, pulling these images from the team build
account will precache the images for CI in us-west-2.  If you want to manually precache these in both us-west-1 and us-west-2, which replicate
to the other commerical regions, run:
```sh
./mirror-cilium.sh v1.16.3
```
