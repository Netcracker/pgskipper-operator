# Building with Cloud Native Buildpacks

This project uses **Cloud Native Buildpacks** to build container images, eliminating the need for Dockerfile maintenance while providing automatic security updates and optimizations.

## What are Cloud Native Buildpacks?

Cloud Native Buildpacks (CNB) transform your application source code into container images without requiring a Dockerfile. They provide:

- **No Dockerfile needed** - Buildpacks detect and configure your application automatically
- **Automatic security updates** - Base images and dependencies stay current
- **Optimized layer caching** - Faster builds with intelligent layer reuse
- **Language best practices** - Production-ready configurations out of the box
- **SBOM generation** - Software Bill of Materials for security compliance
- **Reproducible builds** - Consistent images across environments

## Building the Image

### Using the Build Script (Recommended)

```bash
# From project root
./scripts/build.sh

# With custom image name/tag
IMAGE_NAME=myapp IMAGE_TAG=v1.0 ./scripts/build.sh
```

### Manual Build

```bash
# From spring-app directory
cd spring-app
mvn spring-boot:build-image
```

**Configuration in `pom.xml`:**
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
        </image>
    </configuration>
</plugin>
```

### Alternative: Pack CLI

For advanced use cases, you can use the Pack CLI directly:

```bash
# Install Pack CLI
brew install buildpacks/tap/pack  # macOS

# Build with Pack
cd spring-app
pack build postgresql-failover-test:latest \
    --builder paketobuildpacks/builder:base \
    --path .
```

## Configuration

### Project Descriptor (`project.toml`)

Customize buildpack behavior with `spring-app/project.toml`:

```toml
[build]
  include = ["src/", "pom.xml"]
  exclude = ["target/", ".git/"]

[[build.env]]
name = "BP_JVM_VERSION"
value = "17"

[[build.env]]
name = "BP_JVM_TYPE"
value = "JRE"  # Use JRE for smaller images
```

### Environment Variables

Key buildpack environment variables:

| Variable | Description | Example |
|----------|-------------|---------|
| `BP_JVM_VERSION` | Java version to use | `17`, `21` |
| `BP_JVM_TYPE` | JDK or JRE | `JRE`, `JDK` |
| `BP_MAVEN_BUILD_ARGUMENTS` | Maven build args | `clean package -DskipTests` |
| `BPE_APPEND_JAVA_TOOL_OPTIONS` | Additional JVM options | `-XX:MaxRAMPercentage=75.0` |
| `BP_JVM_JLINK_ENABLED` | Create custom JRE with jlink | `true`, `false` |

### Maven Plugin Configuration

Override settings at build time:

```bash
mvn spring-boot:build-image \
    -Dspring-boot.build-image.imageName=myapp:1.0 \
    -Dspring-boot.build-image.builder=paketobuildpacks/builder:tiny \
    -Dspring-boot.build-image.env.BP_JVM_VERSION=21
```

## Available Builders

Different builders for different use cases:

| Builder | Use Case | Size |
|---------|----------|------|
| `paketobuildpacks/builder:base` | General purpose (default) | ~1GB |
| `paketobuildpacks/builder:full` | All features, languages | ~1.5GB |
| `paketobuildpacks/builder:tiny` | Minimal, distroless | ~500MB |

Example using tiny builder:

```bash
mvn spring-boot:build-image \
    -Dspring-boot.build-image.builder=paketobuildpacks/builder:tiny
```

## Key Advantages

### 1. Zero Maintenance
- No Dockerfile to maintain
- Security patches applied automatically on rebuild
- Language runtime updates handled by buildpack

### 2. Optimized Caching
Buildpacks create intelligent layers automatically:
- Dependencies layer (cached unless pom.xml changes)
- Application layer (cached unless source changes)
- JRE layer (cached unless version changes)

### 3. Security & Compliance
```bash
# Extract SBOM (Software Bill of Materials)
docker run --rm postgresql-failover-test:latest \
    cat /layers/sbom/launch/paketo-buildpacks_*/sbom.syft.json > sbom.json

# Analyze for vulnerabilities
grype postgresql-failover-test:latest
```

### 4. Reproducible Builds
Same source code + same buildpack version = identical image
- Useful for debugging
- Compliance requirements
- Audit trails

## Integration with CI/CD

### GitHub Actions

```yaml
- name: Build with Buildpacks
  run: |
    mvn spring-boot:build-image \
      -Dspring-boot.build-image.imageName=${{ secrets.REGISTRY }}/app:${{ github.sha }}

- name: Push image
  run: docker push ${{ secrets.REGISTRY }}/app:${{ github.sha }}
```

### GitLab CI

```yaml
build:
  image: maven:3.9-eclipse-temurin-17
  script:
    - mvn spring-boot:build-image -DskipTests
  services:
    - docker:dind
```

### Jenkins

```groovy
stage('Build') {
    steps {
        sh 'mvn spring-boot:build-image'
    }
}
```

## Troubleshooting

### Issue: Build fails with "Cannot connect to Docker daemon"

**Solution:** Ensure Docker is running:
```bash
docker ps
```

### Issue: Build is slow on first run

**Solution:** This is normal. Buildpacks download and cache dependencies. Subsequent builds will be much faster.

### Issue: Want smaller images

**Solution 1:** Use tiny builder:
```bash
mvn spring-boot:build-image \
    -Dspring-boot.build-image.builder=paketobuildpacks/builder:tiny
```

**Solution 2:** Enable jlink:
```xml
<env>
    <BP_JVM_JLINK_ENABLED>true</BP_JVM_JLINK_ENABLED>
    <BP_JVM_JLINK_ARGS>--no-man-pages --no-header-files --strip-debug</BP_JVM_JLINK_ARGS>
</env>
```

### Issue: Need custom buildpack

**Solution:** Add custom buildpack:
```xml
<buildpacks>
    <buildpack>gcr.io/paketo-buildpacks/java</buildpack>
    <buildpack>docker://my-custom-buildpack:latest</buildpack>
</buildpacks>
```

## Advanced Features

### Layer Analysis

Inspect image layers:
```bash
pack inspect-image postgresql-failover-test:latest
```

### Rebasing (Update base image without rebuild)

```bash
pack rebase postgresql-failover-test:latest \
    --run-image paketobuildpacks/run:base-cnb
```

### Build from Git

```bash
pack build postgresql-failover-test:latest \
    --builder paketobuildpacks/builder:base \
    --path https://github.com/user/repo.git
```

### Multi-arch Builds

```bash
mvn spring-boot:build-image \
    -Dspring-boot.build-image.platforms=linux/amd64,linux/arm64
```

## Quick Start

1. **Build the image:**
   ```bash
   ./scripts/build-with-buildpacks.sh
   ```

2. **Run locally:**
   ```bash
   docker run -p 8080:8080 \
       -e DATABASE_URL=jdbc:postgresql://localhost:5432/testdb \
       postgresql-failover-test:latest
   ```

3. **Deploy to Kubernetes:**
   ```bash
   kubectl apply -f k8s/deployment.yaml
   ```

## Resources

- [Cloud Native Buildpacks](https://buildpacks.io/)
- [Paketo Buildpacks](https://paketo.io/)
- [Spring Boot Buildpacks](https://docs.spring.io/spring-boot/docs/current/maven-plugin/reference/htmlsingle/#build-image)
- [Pack CLI Reference](https://buildpacks.io/docs/tools/pack/)

## Quick Start

```bash
# 1. Build the image
./scripts/build.sh

# 2. Run locally with Docker
docker run -p 8080:8080 \
    -e DATABASE_URL=jdbc:postgresql://localhost:5432/testdb \
    postgresql-failover-test:latest

# 3. Test the application
curl http://localhost:8080/api/health

# 4. Or deploy to Kubernetes
./scripts/setup.sh
```
