ARG LLAMA_CPP_COMMIT=1d1d9a9ed7a4f09c4225ea4cc8fd3bd1cf2c940f
FROM debian:bookworm-slim AS llama-build
ARG LLAMA_CPP_COMMIT
ARG APT_MIRROR=http://deb.debian.org/debian
ARG APT_SECURITY_MIRROR=http://security.debian.org/debian-security
COPY ai/vendor/llama.cpp-1d1d9a9.tar.gz /tmp/llama.cpp.tar.gz
RUN echo '2a1635cedf202f865d6c4f11cc76af2caf8a8931225e8d80e8d2f457ef4e3151  /tmp/llama.cpp.tar.gz' | sha256sum -c - \
 && sed -i "s|http://deb.debian.org/debian|${APT_MIRROR}|g; s|http://security.debian.org/debian-security|${APT_SECURITY_MIRROR}|g" /etc/apt/sources.list.d/debian.sources \
 && apt-get update && apt-get install -y --no-install-recommends cmake ninja-build g++ \
 && mkdir -p /src && tar -xzf /tmp/llama.cpp.tar.gz --strip-components=1 -C /src \
 && cd /src \
 && cmake -S . -B build -G Ninja -DCMAKE_BUILD_TYPE=Release -DGGML_NATIVE=ON -DLLAMA_CURL=OFF -DLLAMA_BUILD_TESTS=OFF -DLLAMA_BUILD_EXAMPLES=OFF -DLLAMA_BUILD_SERVER=ON -DLLAMA_BUILD_UI=OFF -DLLAMA_USE_PREBUILT_UI=OFF \
 && cmake --build build --target llama-server -j"$(nproc)"

FROM debian:bookworm-slim
ARG APT_MIRROR=http://deb.debian.org/debian
ARG APT_SECURITY_MIRROR=http://security.debian.org/debian-security
ARG PIP_INDEX_URL=https://pypi.tuna.tsinghua.edu.cn/simple
RUN sed -i "s|http://deb.debian.org/debian|${APT_MIRROR}|g; s|http://security.debian.org/debian-security|${APT_SECURITY_MIRROR}|g" /etc/apt/sources.list.d/debian.sources \
 && apt-get update && apt-get install -y --no-install-recommends ffmpeg libgomp1 curl python3 python3-pip python3-venv \
 && rm -rf /var/lib/apt/lists/*
COPY --from=llama-build /src/build/bin/llama-server /usr/local/bin/llama-server
COPY --from=llama-build /src/build/bin/*.so* /usr/local/lib/
COPY ai/requirements.txt /app/requirements.txt
RUN python3 -m venv /opt/venv && /opt/venv/bin/pip install --no-cache-dir --index-url "$PIP_INDEX_URL" -r /app/requirements.txt
COPY ai/app.py ai/taxonomy.py ai/media_sampling.py ai/tag_logic.py ai/tag_hierarchy.py ai/entrypoint.sh /app/
RUN chmod +x /app/entrypoint.sh && useradd --system --uid 10001 --create-home ai
ENV HF_HUB_OFFLINE=1 TRANSFORMERS_OFFLINE=1 TOKENIZERS_PARALLELISM=false LD_LIBRARY_PATH=/usr/local/lib PATH=/opt/venv/bin:$PATH
USER ai
WORKDIR /app
EXPOSE 8090
ENTRYPOINT ["/app/entrypoint.sh"]
