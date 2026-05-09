package commercial

import (
	"testing"

	"ecommerce-service/internal/platform"
)

func TestResolveOrderBundleMatchesPackageBySKUMetadataPackageCode(t *testing.T) {
	svc := &Service{productCode: "ecommerce"}
	offerings := &platform.OfferingsView{
		Packages: []platform.CommercialPackage{
			{ID: "pkg-growth", Code: "ecommerce.pkg.sub.growth.monthly", Name: "Growth Monthly", PackageType: "subscription", Status: "active", Metadata: `{"tier":"growth","monthly_quota":10000}`},
		},
		SKUs: []platform.SKU{
			{ID: "sku-growth", Code: "ecommerce.sku.sub.growth.monthly", Name: "Ecommerce Growth Monthly", SKUType: "subscription", Currency: "CNY", ListPrice: 29900, Status: "active", Metadata: `{"package_code":"ecommerce.pkg.sub.growth.monthly"}`},
		},
	}

	bundle, err := svc.resolveOrderBundle(offerings, "", "ecommerce.pkg.sub.growth.monthly")
	if err != nil {
		t.Fatalf("resolveOrderBundle returned error: %v", err)
	}
	if bundle.Package == nil || bundle.Package.Code != "ecommerce.pkg.sub.growth.monthly" {
		t.Fatalf("unexpected package: %#v", bundle.Package)
	}
	if bundle.SKU == nil || bundle.SKU.Code != "ecommerce.sku.sub.growth.monthly" {
		t.Fatalf("unexpected sku: %#v", bundle.SKU)
	}
	if bundle.UnitAmount != 29900 {
		t.Fatalf("unexpected unit amount: %d", bundle.UnitAmount)
	}
}
