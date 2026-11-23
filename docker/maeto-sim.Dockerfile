FROM python:3.12-slim

WORKDIR /app

RUN apt-get update \
    && apt-get install -y --no-install-recommends iproute2 util-linux \
    && rm -rf /var/lib/apt/lists/*

COPY services/maeto-sim/requirements.txt /tmp/sim-requirements.txt
COPY clab/requirements.txt /tmp/clab-requirements.txt
RUN pip install --no-cache-dir -r /tmp/sim-requirements.txt -r /tmp/clab-requirements.txt

COPY clab/tfsim /app/clab/tfsim
COPY services/maeto-sim /app/sim

ENV MAETO_ROOT=/app PYTHONUNBUFFERED=1

CMD ["python", "/app/sim/main.py"]
