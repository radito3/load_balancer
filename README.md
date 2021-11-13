# load_balancer
TCP Load Balancer

## How to set up Concourse

Execute the following lines in the `concourse` folder of the repo:
```bash
./generate-keys.sh --use-pem
docker-compose up -d
```

## How to stop Concourse

From the `concourse` folder of the repo:
```bash
docker-compose down
```
