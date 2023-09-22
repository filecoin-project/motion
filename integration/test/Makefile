.PHONY: devnet/up devnet/down motionlarity/up motionlarity/down test

./devnet/.up:
	echo "Run Boost Devnet from pre-built images"
	rm -rf ./devnet/data && docker compose -f ./devnet/docker-compose.yaml up -d
	echo "Await Lotus full node startup"
	docker compose -f ./devnet/docker-compose.yaml exec lotus lotus wait-api --timeout=20m
	echo "Await Lotus-miner full node startup"
	docker compose -f ./devnet/docker-compose.yaml exec lotus-miner lotus-miner wait-api --timeout=20m
	touch ./devnet/.up

devnet/up: ./devnet/.up

./devnet/.boostready: ./devnet/.up
	./boost-setup.sh
	touch ./devnet/.boostready

./motionlarity/.env.local: ./devnet/.up
	echo "Set up Motion Wallet and API endpoints"
	cat ./motionlarity/.env > ./motionlarity/.env.local
	echo "" >> ./motionlarity/.env.local
	./integration-setup.sh ./motionlarity/.env.local

motionlarity/setup: ./motionlarity/.env.local ./devnet/.boostready

devnet/down: motionlarity/down
	docker compose -f ./devnet/docker-compose.yaml down && sleep 2 && rm -rf ./devnet/data
	rm ./devnet/.up || true

./motionlarity/.up: ./motionlarity/.env.local ./devnet/.boostready
	./buildsingularity.sh
	echo "Run Motionlarity"
	docker compose -f ./motionlarity/docker-compose.yaml --env-file ./motionlarity/.env.local up -d
	./waitmotion.sh
	touch ./motionlarity/.up

motionlarity/up: ./motionlarity/.up

motionlarity/down:
	docker compose -f ./motionlarity/docker-compose.yaml down --rmi=all --volumes
	rm ./motionlarity/.up || true
	rm ./motionlarity/.env.local || true
	rm ./devnet/.boostready || true

test: motionlarity/up
	echo "Run Motion integration tests"
	MOTION_INTEGRATION_TEST='true' go test . -v --count=1
