# คำสั่งที่ใช้ในการ Test
cd backend && go test -v ./test/...

# ทดสอบ Intergration Test
cd backend && \
set -a && source ../.env && set +a && \
TEST_API_URL=http://localhost:8080/api/v1 go test -v ./test/...