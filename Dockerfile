ARG BUILDER_IMAGE

FROM ${BUILDER_IMAGE} AS build

ARG TARGETARCH

WORKDIR /build
COPY . .

RUN mkdir /artifacts
RUN make PREFIX=/artifacts cmds

RUN cp ./deployments/container/reconfigure-mig.sh /artifacts/reconfigure-mig.sh

ARG KUBECTL_VERSION=v1.34.5-dd.1

RUN curl -Lfs https://github.com/DataDog/kubernetes/releases/download/${KUBECTL_VERSION}/kubernetes-server-linux-${TARGETARCH}.tar.gz -O
RUN tar -C /usr/local/bin/ --strip-components 3 --exclude '*.tar' --exclude '*.docker_tag' -xvzf kubernetes-server-linux-${TARGETARCH}.tar.gz kubernetes/server/bin/kubectl
RUN chmod 755 /usr/local/bin/kubectl
RUN go tool nm /usr/local/bin/kubectl | grep -E 'sig.FIPSOnly'

# Install the nvidia-ctk binary as a go executable
# TODO: Once we can consume a golang API from reconfigure.sh we can remove this.
ARG NVIDIA_CTK_VERSION=datadog
RUN git clone --branch "${NVIDIA_CTK_VERSION}" --single-branch https://github.com/DataDog/nvidia-container-toolkit.git /container-toolkit
RUN cd /container-toolkit && make PREFIX=./dist cmd-nvidia-ctk
RUN cp /container-toolkit/dist/nvidia-ctk /artifacts/nvidia-ctk

FROM registry.ddbuild.io/images/nvidia-cuda-base:12.9.0

LABEL maintainers="Compute"

# Entrypoint configmap is mounted in the container
USER root

COPY --from=build /artifacts/nvidia-mig-parted  /usr/bin/nvidia-mig-parted
COPY --from=build /artifacts/nvidia-mig-manager /usr/bin/nvidia-mig-manager
COPY --from=build /artifacts/reconfigure-mig.sh /usr/bin/reconfigure-mig.sh
COPY --from=build /usr/local/bin/kubectl        /usr/bin/kubectl
COPY --from=build /artifacts/nvidia-ctk         /usr/bin/nvidia-ctk

ENV NVIDIA_DISABLE_REQUIRE="true"
ENV NVIDIA_VISIBLE_DEVICES=all
ENV NVIDIA_MIG_CONFIG_DEVICES=all
ENV NVIDIA_DRIVER_CAPABILITIES=utility

ENTRYPOINT ["nvidia-mig-manager"]
