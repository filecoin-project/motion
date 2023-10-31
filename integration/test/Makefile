.PHONY: devnet/up devnet/down motionlarity/up motionlarity/down test

./devnet/.up:
	@echo "Running Boost Devnet from pre-built images"
	@rm -rf ./devnet/data || (echo "\033[1mCould not clean devnet\033[0m, try running 'sudo rm -rf ./devnet/data'" && false)
	docker compose -f ./devnet/docker-compose.yaml up -d
	@echo "Awaiting Lotus full node startup..."
	docker compose -f ./devnet/docker-compose.yaml exec lotus lotus wait-api --timeout=20m
	@echo "Lotus full node started"
	@echo "Awaiting Lotus-miner full node startup..."
	docker compose -f ./devnet/docker-compose.yaml exec lotus-miner lotus-miner wait-api --timeout=20m
	@echo "Lotus-miner full node started"
	touch ./devnet/.up

devnet/up: ./devnet/.up

./devnet/.boostready: ./devnet/.up
	./boost-setup.sh
	touch ./devnet/.boostready

./motionlarity/.env.local: ./devnet/.up
	@echo "Setting up Motion Wallet and API endpoints"
	cat ./motionlarity/.env > ./motionlarity/.env.local
	echo "" >> ./motionlarity/.env.local
	./integration-setup.sh ./motionlarity/.env.local

motionlarity/setup: ./motionlarity/.env.local ./devnet/.boostready

devnet/down:
	docker compose -f ./devnet/docker-compose.yaml down
	sleep 2
	rm -f ./devnet/.up || true
	@rm -rf ./devnet/data || (echo "\033[31mCould not clean devnet\033[0m, try running 'sudo rm -rf ./devnet/data'" && false)

./motionlarity/.up: ./motionlarity/.env.local ./devnet/.boostready
	./buildsingularity.sh
	@echo "Runing Motionlarity"
	docker compose -f ./motionlarity/docker-compose.yaml --env-file ./motionlarity/.env.local up -d
	./waitmotion.sh
	touch ./motionlarity/.up

motionlarity/up: ./motionlarity/.up

motionlarity/down: devnet/down
	docker compose -f ./motionlarity/docker-compose.yaml down --rmi=all --volumes
	rm -f ./motionlarity/.up || true
	rm -f ./motionlarity/.env.local || true
	rm -f ./devnet/.boostready || true

test: motionlarity/up
	@echo "Running Motion integration tests"
	MOTION_INTEGRATION_TEST='true' go test . -v --count=1
