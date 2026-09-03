FROM python:3.13-slim AS base
WORKDIR /srv
COPY apps/rag-service/pyproject.toml ./
COPY apps/rag-service/app ./app
COPY apps/rag-service/migrations ./migrations
RUN pip install --no-cache-dir .
EXPOSE 8099
CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0", "--port", "8099"]