FROM python:3.12-slim

WORKDIR /app

COPY requirements.txt .

RUN python -m pip install \
    --no-cache-dir \
    --disable-pip-version-check \
    -r requirements.txt \
    && python -m pip check

COPY initialize_model.py .

ENV HOME=/tmp \
    PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    HF_HUB_DISABLE_PROGRESS_BARS=1

USER 10001:10001

ENTRYPOINT ["python", "/app/initialize_model.py"]
