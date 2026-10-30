FROM vllm/vllm-openai-cpu@sha256:ffaba3989b9b96d95a576143c872a4110ce9800f5199b6a36eb27bdff1cbbe05

COPY requirements.txt /opt/vllm/requirements.txt
COPY logging.json /etc/vllm/logging.json

RUN python -m pip freeze | diff -u /opt/vllm/requirements.txt -

ENV HOME=/tmp
ENV VLLM_NO_USAGE_STATS=1

USER 10001:10001

EXPOSE 8000

ENTRYPOINT ["vllm", "serve"]
