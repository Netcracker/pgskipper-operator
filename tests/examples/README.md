# pgskipper-operator Examples

This directory contains example projects demonstrating various use cases for pgskipper-operator.

## Available Examples

### [Spring Boot Failover Testing](spring-boot-failover-test/)

A comprehensive testing environment to reproduce and analyze PostgreSQL failover behavior with Spring Boot applications using HikariCP connection pooling.

**Features:**
- PostgreSQL HA cluster with automatic failover
- Spring Boot application with connection monitoring
- Failover testing scripts
- Detailed metrics and monitoring
- HikariCP connection pool optimization

**Use Cases:**
- Testing application behavior during database failover
- Validating connection pool configurations
- Analyzing reconnection patterns and timing
- Developing failover-resilient applications

**Quick Start:**
```bash
cd spring-boot-failover-test
helmfile sync
```

See the [full documentation](spring-boot-failover-test/README.md) for detailed instructions.

---

## Contributing Examples

If you have an example demonstrating a specific use case for pgskipper-operator, please feel free to contribute it! Each example should include:

1. **README.md** - Clear documentation with prerequisites, setup instructions, and usage
2. **helmfile.yaml** or deployment manifests - Reproducible deployment configuration
3. **Application code** - If applicable, well-documented source code
4. **.gitignore** - To exclude build artifacts and sensitive data

## Support

For issues or questions about these examples:
- Check the individual example's documentation
- Review the main [pgskipper-operator documentation](../../README.md)
- Open an issue in the [GitHub repository](https://github.com/Netcracker/pgskipper-operator/issues)
