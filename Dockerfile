ARG BUILDER_IMAGE

FROM ${BUILDER_IMAGE} AS build

WORKDIR /build
COPY . .

RUN mkdir /artifacts
RUN make PREFIX=/artifacts cmds

RUN cp ./deployments/container/reconfigure-mig.sh /artifacts/reconfigure-mig.sh

RUN curl -o /usr/bin/kubectl -L "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/${TARGETARCH}/kubectl";
RUN chmod a+x /usr/bin/kubectl

# Install the nvidia-ctk binary as a go executable
# TODO: Once we can consume a golang API from reconfigure.sh we can remove this.
ARG NVIDIA_CTK_VERSION=main
RUN go install -ldflags "-extldflags=-Wl,-z,lazy -s -w" \
    github.com/NVIDIA/nvidia-container-toolkit/cmd/nvidia-ctk@${NVIDIA_CTK_VERSION} \
    && cp /go/bin/nvidia-ctk /artifacts/nvidia-ctk

FROM registry.ddbuild.io/images/nvidia-cuda-base:12.9.0

LABEL maintainers="Compute"

COPY --from=build /artifacts/nvidia-mig-parted  /usr/bin/nvidia-mig-parted
COPY --from=build /artifacts/nvidia-mig-manager /usr/bin/nvidia-mig-manager
COPY --from=build /artifacts/reconfigure-mig.sh /usr/bin/reconfigure-mig.sh
COPY --from=build /usr/bin/kubectl              /usr/bin/kubectl
COPY --from=build /artifacts/nvidia-ctk         /usr/bin/nvidia-ctk

ENV NVIDIA_DISABLE_REQUIRE="true"
ENV NVIDIA_VISIBLE_DEVICES=all
ENV NVIDIA_MIG_CONFIG_DEVICES=all
ENV NVIDIA_DRIVER_CAPABILITIES=utility

ENTRYPOINT ["nvidia-mig-manager"]
