FROM python:3.12-alpine
RUN pip install --no-cache-dir pyyaml
WORKDIR /app
COPY server.py .
COPY static/ static/
ENV DEVBOARD_DATA=/data DEVBOARD_PORT=8484
EXPOSE 8484
CMD ["python", "server.py"]
