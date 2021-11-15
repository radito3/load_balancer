for ((i=0; i<3; i++)); do
    echo "ping 1" | nc -q 0 127.0.0.1 80
    echo "ping 2" | nc -q 0 127.0.0.1 80
    echo "ping 3" | nc -q 0 127.0.0.1 80
done
