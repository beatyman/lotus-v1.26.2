rust-fil-proofs自定义版本
```shell
cd ~/go/src/github.com/filecoin-project/lotus

cp filecoin-ffi/ffi.toml extern/filecoin-ffi/rust/Cargo.toml

git clone --origin fivestar https://github.com/filecoin-fivestar/rust-filecoin-proofs-api ../
cd ../rust-filecoin-proofs-api
git checkout v5.4.1-patch1 # git pull fivestar更新到最新
cd -
cp filecoin-ffi/api.toml ../rust-filecoin-proofs-api/Cargo.toml

git clone --origin fivestar https://github.com/filecoin-fivestar/rust-fil-proofs ../
cd ../rust-fil-proofs
git checkout v5.4.0 # git pull fivestar更新到最新

cd -
make clean
./install.sh
```
