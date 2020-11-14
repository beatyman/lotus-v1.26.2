
cd ~/go/src/github.com/filecoin-project/lotus

cp filecoin-ffi/ffi.toml extern/filecoin-ffi/rust/Cargo.toml

git clone https://github.com/filecoin-project/rust-filecoin-proofs-api ../
cd ../rust-filecoin-proofs-api
git checkout v5.3.0
cd -
cp filecoin-ffi/api.toml ../rust-filecoin-proofs-api/Cargo.toml

git clone https://github.com/filecoin-fivestar/rust-fil-proofs ../
cd ../rust-fil-proofs
git checkout v5.3.0

cd -
make clean
./install.sh
