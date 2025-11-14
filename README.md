# sshor

A simple onion-routing implementation over SSH for educational purposes.

## Overview

`sshor` demonstrates onion routing concepts using SSH as the transport layer. This is an educational project and **not intended for production use**.

## Modules

- **Library (`go-i2p/sshor`)**: Core onion routing functionality
- **CLI (`go-i2p/sshor/sshor`)**: Command-line interface for running the onion router

## Installation

```bash
go get github.com/go-i2p/sshor
go install github.com/go-i2p/sshor/sshor@latest
```

## Usage

```bash
sshor [options]
```

## Warning

This implementation is for educational purposes only. It is not secure for real-world applications.

## License

See LICENSE file for details.
