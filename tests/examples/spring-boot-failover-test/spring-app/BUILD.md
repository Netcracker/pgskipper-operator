# Build Instructions

## Quick Start

This project uses **Cloud Native Buildpacks** to build container images.

```bash
# From project root
./scripts/build.sh

# Or from spring-app directory
mvn spring-boot:build-image
```

## Build Features

- ✅ **No Dockerfile needed** - Buildpacks auto-configure everything
- ✅ **Automatic security updates** - Rebuild to get latest patches
- ✅ **Optimized layer caching** - Faster builds automatically
- ✅ **SBOM included** - Software Bill of Materials for compliance
- ✅ **Production-ready defaults** - Best practices applied automatically

## Build Commands

| Task | Command |
|------|---------|
| **Build image** | `mvn spring-boot:build-image` |
| **Custom tag** | `-Dspring-boot.build-image.imageName=app:v1` |
| **Set Java version** | `-Dspring-boot.build-image.env.BP_JVM_VERSION=21` |
| **Use tiny builder** | `-Dspring-boot.build-image.builder=paketobuildpacks/builder:tiny` |
| **Build speed (first)** | ~3-5 min |
| **Build speed (cached)** | ~30-60 sec |
| **Image size** | ~300-400 MB (base), ~280 MB (tiny) |

## Configuration Files

### Buildpacks Configuration

**`pom.xml` (Maven Plugin)**
```xml
<plugin>
    <groupId>org.springframework.boot</groupId>
    <artifactId>spring-boot-maven-plugin</artifactId>
    <configuration>
        <image>
            <name>postgresql-failover-test:${project.version}</name>
            <env>
                <BP_JVM_VERSION>17</BP_JVM_VERSION>
                <BPE_APPEND_JAVA_TOOL_OPTIONS>-XX:+UseContainerSupport -XX:MaxRAMPercentage=75.0</BPE_APPEND_JAVA_TOOL_OPTIONS>
            </env>
            <buildpacks>
                <buildpack>gcr.io/paketo-buildpacks/java</buildpack>
            </buildpacks>
        </image>
    </configuration>
</plugin>
```

**`project.toml` (Optional - for Pack CLI)**
```toml
[build]
  include = ["src/", "pom.xml"]

[[build.env]]
name = "BP_JVM_VERSION"
value = "17"

[[build.env]]
name = "BPE_APPEND_JAVA_TOOL_OPTIONS"
value = "-XX:+UseContainerSupport -XX:MaxRAMPercentage=75.0"
```

## Environment Variables

### Buildpacks Environment Variables

| Variable | Description | Default | Example |
|----------|-------------|---------|---------|
| `BP_JVM_VERSION` | Java version | `17` | `21` |
| `BP_JVM_TYPE` | JRE or JDK | `JRE` | `JDK` |
| `BP_MAVEN_BUILD_ARGUMENTS` | Maven args | `package` | `clean package -DskipTests` |
| `BPE_APPEND_JAVA_TOOL_OPTIONS` | JVM options | - | `-XX:MaxRAMPercentage=75.0` |
| `BP_JVM_JLINK_ENABLED` | Custom JRE with jlink | `false` | `true` |

Set at build time:
```bash
mvn spring-boot:build-image \
    -Dspring-boot.build-image.env.BP_JVM_VERSION=21
```

Or in `pom.xml`:
```xml
<env>
    <BP_JVM_VERSION>21</BP_JVM_VERSION>
</env>
```

## Local Development

### Run with Docker Compose

```bash
cd spring-app
docker-compose up --build
```

This starts:
- PostgreSQL database
- Spring Boot application (built with buildpacks)

### Run without Docker

```bash
cd spring-app

# Build JAR
mvn clean package

# Run with local PostgreSQL
java -jar target/*.jar \
    --spring.datasource.url=jdbc:postgresql://localhost:5432/testdb \
    --spring.datasource.username=postgres \
    --spring.datasource.password=postgres
```

## CI/CD Examples

### GitHub Actions

```yaml
name: Build Image
on: [push]

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3

      - name: Set up JDK 17
        uses: actions/setup-java@v3
        with:
          java-version: '17'
          distribution: 'temurin'

      - name: Build with Buildpacks
        run: |
          cd spring-app
          mvn spring-boot:build-image \
            -Dspring-boot.build-image.imageName=${{ github.repository }}:${{ github.sha }}

      - name: Push to registry
        run: docker push ${{ github.repository }}:${{ github.sha }}
```

### GitLab CI

```yaml
build:
  image: maven:3.9-eclipse-temurin-17
  services:
    - docker:dind
  script:
    - cd spring-app
    - mvn spring-boot:build-image -DskipTests
  only:
    - main
```

### Jenkins

```groovy
pipeline {
    agent any
    stages {
        stage('Build') {
            steps {
                dir('spring-app') {
                    sh 'mvn spring-boot:build-image'
                }
            }
        }
    }
}
```

## Image Analysis

```bash
# Show buildpack metadata
pack inspect-image postgresql-failover-test:latest

# Extract SBOM (Software Bill of Materials)
docker run --rm postgresql-failover-test:latest \
    cat /layers/sbom/launch/paketo-buildpacks_*/sbom.syft.json > sbom.json

# Analyze for vulnerabilities
grype postgresql-failover-test:latest

# Show image layers
docker history postgresql-failover-test:latest
```

## Troubleshooting

### Build fails with "Cannot connect to Docker daemon"

```bash
# Check Docker is running
docker ps

# On macOS/Windows, ensure Docker Desktop is running
```

### Maven build fails

```bash
# Clean and retry
mvn clean
mvn spring-boot:build-image
```

### Out of disk space

```bash
# Clean Docker cache
docker system prune -a

# Clean Maven cache
rm -rf ~/.m2/repository
```

### Want smaller images

**Option 1: Use tiny builder**
```bash
mvn spring-boot:build-image \
    -Dspring-boot.build-image.builder=paketobuildpacks/builder:tiny
```

**Option 2: Enable jlink (custom JRE)**
```xml
<env>
    <BP_JVM_JLINK_ENABLED>true</BP_JVM_JLINK_ENABLED>
</env>
```

## Performance Tips

1. **Use buildpacks for consistency** - Same image every time
2. **Enable layer caching** - Automatic with buildpacks
3. **Skip tests during image build** - Run tests separately
4. **Use `.dockerignore`** - Exclude unnecessary files
5. **Multi-stage builds** - Already optimized in buildpacks

## Security Best Practices

1. **SBOM Generation** - Buildpacks create it automatically
2. **Vulnerability Scanning** - Use `grype` or `trivy`
3. **Non-root user** - Both approaches use non-root
4. **Minimal base images** - Use `tiny` builder for buildpacks
5. **Regular updates** - Rebuild images weekly

## Further Reading

- [Spring Boot Buildpacks Documentation](https://docs.spring.io/spring-boot/docs/current/maven-plugin/reference/htmlsingle/#build-image)
- [Paketo Buildpacks](https://paketo.io/)
- [Docker BuildKit](https://docs.docker.com/build/buildkit/)
- [Cloud Native Buildpacks](https://buildpacks.io/)
