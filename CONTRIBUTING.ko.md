# `goaugtype` 기여 가이드

[English](./CONTRIBUTING.md) | [한국어](./CONTRIBUTING.ko.md)

`goaugtype` 프로젝트에 관심을 가져주셔서 감사합니다! 본 문서는 레포지토리 구조, Go 표준 라이브러리 `src/go/types/` 동기화 및 패치 파이프라인, 그리고 기여자를 위한 단계별 개발 워크플로우를 설명합니다.

---

## 목차

- [레포지토리 구조](#레포지토리-구조)
- [1:1 업스트림 매핑 테이블](#11-업스트림-매핑-테이블)
- [개발자 워크플로우](#개발자-워크플로우)
  - [1. 테스트 실행](#1-테스트-실행)
  - [2. 무수정 원본 스냅샷으로부터 포팅 파일 재생성](#2-무수정-원본-스냅샷으로부터-포팅-파일-재생성)
  - [3. 포팅된 `go/types` 파일 수정 및 패치 갱신](#3-포팅된-gotypes-파일-수정-및-패치-갱신)
  - [4. 새로운 Go 업스트림 릴리즈 동기화](#4-새로운-go-업스트림-릴리즈-동기화)
  - [5. 릴리즈 검증 및 태그 발행](#5-릴리즈-검증-및-태그-발행)
- [엔지니어링 가이드라인](#엔지니어링-가이드라인)
- [관련 문서](#관련-문서)

---

## 레포지토리 구조

```
goaugtype/
├── cmd/
│   ├── goaugadt/               # 단독 실행형 CLI 드라이버 (singlechecker 기반)
│   └── upstreamchecker/        # 업스트림 드리프트 감지기, 스냅샷 및 패치 관리기
├── pkg/
│   └── analyzer/               # 코어 정적 분석 패키지
│       ├── adt.go              # Registry, SumTypeSet, SumTypeFact, //go:generate
│       ├── check.go            # AST 수집기, 8대 대입 경로 검증, type switch 검증
│       ├── doc.go              # analyzer 패키지 문서
│       ├── analyzer_test.go    # analysistest 테스트 러너
│       ├── testdata/           # 정적 테스트 케이스 (simple, imported, typeset 등)
│       ├── typeterm.go         # gotypes/typeterm.go + typeterm.patch로 생성되는 패치 파일
│       ├── termlist.go         # gotypes/termlist.go + termlist.patch로 생성되는 패치 파일
│       └── {typeset,union,instantiate,operand,assignments}.go # go/types 모델 기반 자체 어댑터 파일
├── internal/
│   └── upstream/
│       ├── testdata/gotypes/   # src/go/types 원본 7개 파일의 100% 무수정 스냅샷 (바이트 단위 일치)
│       └── patches/            # 패치 대상 파일(typeterm, termlist)에 대한 Unified Diff (.patch) 파일
├── scripts/
│   ├── check-upstream.sh       # 5중 무결성 및 패치 드리프트 검증 스크립트
│   ├── apply-patches.sh        # 원본 스냅샷 + 패치로부터 pkg/analyzer/*.go 재생성
│   ├── update-patches.sh       # pkg/analyzer/ 로컬 변경사항으로부터 패치 역생성
│   ├── sync-upstream.sh        # go.mod 버전과 스냅샷 동기화 및 3-way 병합
│   └── release.sh              # 릴리즈 게이트키퍼 파이프라인 (--check-only 또는 <tag>)
├── docs/                       # 설계 명세서, 아키텍처 문서 및 다이어그램
├── upstream.json               # 선언적 매핑 설정 (디렉토리, base_revision, files[] 및 SHA-256)
└── go.mod
```

> [!IMPORTANT]
> 무수정 Go 표준 라이브러리 원본 파일들은 `internal/upstream/testdata/gotypes/` 경로에 저장됩니다. 디렉토리 경로에 `testdata`가 포함되어 있으므로 Go 빌드 툴체인, `go test`, `gopls`가 `package types` 선언을 안전하게 무시하며, 빌드 오염 없이 100% 무수정 상태로 유지됩니다.

---

## 1:1 업스트림 매핑 테이블

`goaugtype`은 Go 컴파일러 타입 체커(`src/go/types/`)의 핵심 파일 7개를 두 가지 범주로 나누어 관리합니다:
1. **패치 후 사용 파일 (`typeterm.go`, `termlist.go`)**: 원본 알고리즘을 그대로 유지한 채 최소 변경 패치(`internal/upstream/patches/*.patch`)를 적용하여 `go generate ./...` 또는 `./scripts/apply-patches.sh`로 생성합니다.
2. **감시 전용 참조 파일 (`typeset.go`, `union.go`, `instantiate.go`, `operand.go`, `assignments.go`)**: 업스트림 스펙/구현 변경을 감지하기 위해 무수정 원본 스냅샷(`snapshot` + `sha256`)만 추적하며, `pkg/analyzer/` 내 대응 파일들은 `*types.Info` 위에서 동작하는 `goaugtype` 고유 소스 코드로 직접 관리합니다.

| 업스트림 `src/go/types/` 파일 | 무수정 원본 스냅샷 (`internal/upstream/testdata/gotypes/`) | 추적 모드 / Unified Diff 패치 | 분석기 파일 (`pkg/analyzer/`) | 주요 심볼 및 `[goaugtype]` 역할 |
| :--- | :--- | :--- | :--- | :--- |
| `typeterm.go` | [`gotypes/typeterm.go`](./internal/upstream/testdata/gotypes/typeterm.go) | **패치 적용** ([`typeterm.patch`](./internal/upstream/patches/typeterm.patch)) | [`typeterm.go`](./pkg/analyzer/typeterm.go) | `term`, `equal`, `union`, `intersect`, `includes`, `subsetOf`, `disjoint` — 비어 있지 않은 일반 인터페이스 항 지원 (`#5`). |
| `termlist.go` | [`gotypes/termlist.go`](./internal/upstream/testdata/gotypes/termlist.go) | **패치 적용** ([`termlist.patch`](./internal/upstream/patches/termlist.patch)) | [`termlist.go`](./pkg/analyzer/termlist.go) | `termlist`, `allTermlist`, `norm`, `union`, `intersect`, `equal`, `includes`, `supersetOf`, `subsetOf` — 원본 알고리즘 100% 보존. |
| `typeset.go` | [`gotypes/typeset.go`](./internal/upstream/testdata/gotypes/typeset.go) | **감시 전용** (snapshot-only) | [`typeset.go`](./pkg/analyzer/typeset.go) | `_TypeSet`, `IsEmpty`, `IsAll`, `IsMethodSet`, `NumMethods`, `hasTerms`, `subsetOf`, `computeInterfaceTypeSet`. |
| `union.go` | [`gotypes/union.go`](./internal/upstream/testdata/gotypes/union.go) | **감시 전용** (snapshot-only) | [`pkg/analyzer/union.go`](./pkg/analyzer/union.go) | `parseTilde`(`~T` 및 `nil` 금지), `overlappingTerm`(서로소 검증), `computeUnionTypeSet`(중첩 합타입 재귀 평탄화). |
| `instantiate.go` | [`gotypes/instantiate.go`](./internal/upstream/testdata/gotypes/instantiate.go) | **감시 전용** (snapshot-only) | [`pkg/analyzer/instantiate.go`](./pkg/analyzer/instantiate.go) | `(reg *Registry) implements` — 메서드 규약 검증(`#9`) 및 타입 셋 포함/부분집합 판정. |
| `operand.go` | [`gotypes/operand.go`](./internal/upstream/testdata/gotypes/operand.go) | **감시 전용** (snapshot-only) | [`pkg/analyzer/operand.go`](./pkg/analyzer/operand.go) | `operandMode`, `operand`, `newOperand`, `(*operand).assignableTo` — `Registry.Lookup`을 후킹하여 합타입 대입 가능성 검증. |
| `assignments.go` | [`gotypes/assignments.go`](./internal/upstream/testdata/gotypes/assignments.go) | **감시 전용** (snapshot-only) | [`assignments.go`](./pkg/analyzer/assignments.go) | `assignment`, `initVar`, `assignVar`, `unpackRecv`, `initVars`, `assignVars`, `returnVars` — 1:1 및 튜플 분해 대입 검증. |

---

## 개발자 워크플로우

### 1. 테스트 실행

전체 패키지에 대해 분석 테스트 스위트를 실행합니다:

```bash
go test -v ./...
```

특정 패키지만 실행하려면:

```bash
go test -v ./pkg/analyzer
```

### 2. 무수정 원본 스냅샷으로부터 패치 파일 재생성

`pkg/analyzer/` 내 패치 대상 파일(`typeterm.go`, `termlist.go`)을 원본 스냅샷과 패치로부터 깨끗하게 재생성합니다:

```bash
# go generate 명령어 사용:
go generate ./...

# 또는 헬퍼 스크립트 사용:
./scripts/apply-patches.sh
```

### 3. 패치 적용 파일(`typeterm.go`, `termlist.go`) 수정 및 패치 갱신

패치로 관리되는 파일(`typeterm.go`, `termlist.go`)의 로직을 수정하거나 버그를 해결할 때의 절차입니다:

1. **대상 파일을 직접 수정**합니다 (`pkg/analyzer/<file>.go`).
2. 변경된 코드 영역을 명확한 주석 헤더로 표기합니다:

   ```go
   // [goaugtype]: 합타입 커스텀 처리 로직...
   ```
3. **패치 파일을 갱신**합니다:

   ```bash
   ./scripts/update-patches.sh
   ```
4. **5중 무결성 및 패치 드리프트를 검증**합니다:

   ```bash
   ./scripts/check-upstream.sh -target local
   ```
5. **테스트를 수행**합니다:

   ```bash
   go test -v ./...
   ```

### 4. 새로운 Go 업스트림 릴리즈 동기화

**원칙**: 기준 버전의 단일 진실 공급원(Single Source of Truth)은 [`go.mod`](./go.mod)의 `go` 버전 지시자입니다. [`pkg/analyzer/adt.go`](./pkg/analyzer/adt.go)의 `UpstreamBaseRevision`과 [`upstream.json`](./upstream.json)의 `base_revision`은 항상 `go.mod`에 선언된 `go` 버전(`"go" + <go.mod 버전>`)에 맞추어야 합니다.

`go.mod`의 Go 버전을 업그레이드할 때(예: `1.28.0`)의 동기화 절차는 다음과 같습니다:

1. [`go.mod`](./go.mod)의 **`go` 버전을 먼저 변경**합니다:
   ```go
   go 1.28.0
   ```
2. [`pkg/analyzer/adt.go`](./pkg/analyzer/adt.go)의 **`UpstreamBaseRevision`을 `go.mod` 버전에 맞춥니다**:
   ```go
   const UpstreamBaseRevision = "go1.28.0"
   ```
3. **스냅샷 동기화 및 3-way 병합을 수행**합니다 (`-target`을 생략해도 `go.mod` 버전을 자동으로 인식합니다):
   ```bash
   ./scripts/sync-upstream.sh
   ```
   이 스크립트는 다음 작업을 순차적으로 수행합니다:
   - `go.mod`에서 대상 Go 버전(`go1.28.0`)을 직접 읽어옵니다.
   - `golang/go` 원격 저장소에서 새 원본 파일을 가져옵니다.
   - `internal/upstream/testdata/gotypes/*.go` 스냅샷을 갱신합니다.
   - `upstream.json` 내 새 SHA-256 해시를 기록합니다.
   - 기존 패치 파일들을 `git apply --3way`로 자동 적용합니다.
4. 3-way 병합으로 라인 오프셋이 이동한 경우 **패치를 다시 저장**합니다:
   ```bash
   ./scripts/update-patches.sh
   ```
5. **전체 파이프라인 무결성을 검증**합니다:
   ```bash
   ./scripts/release.sh --check-only
   ```

### 5. 릴리즈 검증 및 태그 발행

새 버전을 배포하기 전 게이트키퍼 검증 파이프라인을 실행합니다:

```bash
# 드라이 런 검증 (커밋되지 않은 변경사항 허용 모드):
./scripts/release.sh --check-only

# 릴리즈 태그 생성 (워킹 트리가 완전히 깨끗해야 함):
./scripts/release.sh v0.3.0

# 원격 태그 푸시:
git push origin v0.3.0
```

---

## 엔지니어링 가이드라인

1. **`UpstreamBaseRevision`은 `go.mod`의 `go` 버전을 따름**: [`pkg/analyzer/adt.go`](./pkg/analyzer/adt.go)의 `UpstreamBaseRevision` 및 [`upstream.json`](./upstream.json)의 `base_revision`은 항상 [`go.mod`](./go.mod)에 명시된 `go` 버전과 일치해야 합니다(예: `go 1.27.3` → `UpstreamBaseRevision = "go1.27.3"`).
2. **최소 변경 원칙(Minimal Diff)**: 원본 `go/types` 대비 차이점을 최소화하고, 모든 커스텀 수정 영역은 `// [goaugtype]`으로 명시하십시오.
3. **쌍별 서로소 불변식 엄수**: 모든 합타입은 평탄화된 항들 사이의 쌍별 서로소를 반드시 보장해야 합니다. `any`나 서브타입 충돌($C \subset I$, $I_1 \subseteq I_2$)을 절대 허용하지 마십시오.
4. **`gopls` 진단 제로 유지**: 풀 리퀘스트를 제출하기 전 모든 패키지에서 에러 및 경고 진단이 0개인지 확인하십시오.
5. **패치 동기화 필수**: `pkg/analyzer/{typeterm,termlist}.go`를 수정한 경우, 반드시 `./scripts/update-patches.sh`를 실행하여 `internal/upstream/patches/*.patch` 파일도 함께 커밋해야 합니다.

---

## 관련 문서

- [설계 명세서 (`DESIGN.ko.md`)](./docs/DESIGN.ko.md) | [English (`DESIGN.md`)](./docs/DESIGN.md)
- [아키텍처 문서 (`ARCHITECTURE.ko.md`)](./docs/ARCHITECTURE.ko.md) | [English (`ARCHITECTURE.md`)](./docs/ARCHITECTURE.md)
