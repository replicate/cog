FROM python:3.10

RUN mkdir /src
WORKDIR /src

COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt

COPY . .

CMD ["python", "app.py"]
